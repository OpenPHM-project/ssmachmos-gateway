package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
	"github.com/jukuly/ss_machmos/server/internal/out"
)

// Measurements as the gateway would upload them.
var testMeasurementsJSON = []byte(`[{"type":"vibration","x":[0.1,0.2],"y":[0.3,0.4],"z":[0.5,0.6]}]`)

// useTempCacheDir points the gateway's data folders (sent_data, unsent_data)
// at a throwaway folder, so tests never touch the real ~/.cache/ss_machmos.
func useTempCacheDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	if dir, err := os.UserCacheDir(); err != nil || dir != tmp {
		t.Skipf("cannot redirect the cache dir on this OS (got %q, %v)", dir, err)
	}
}

// listenForBroadcasts plays the part of a GUI connected to the gateway.
// It returns a function that stops listening and returns every message
// (like "UPLOAD-SUCCESS") that the gateway broadcast in the meantime.
func listenForBroadcasts(t *testing.T) (stop func() []string) {
	t.Helper()
	gatewaySide, guiSide := net.Pipe()
	conn := net.Conn(gatewaySide)
	out.LoggingConnections[&conn] = true

	done := make(chan []string, 1)
	go func() {
		var msgs []string
		reader := bufio.NewReader(guiSide)
		for {
			msg, err := reader.ReadString(0) // messages look like "MSG:<text>\x00"
			if err != nil {
				break
			}
			msgs = append(msgs, strings.TrimSuffix(strings.TrimPrefix(msg, "MSG:"), "\x00"))
		}
		done <- msgs
	}()

	var got []string
	stopped := false
	stop = func() []string {
		if !stopped {
			stopped = true
			delete(out.LoggingConnections, &conn)
			gatewaySide.Close() // lets the reader finish after the last message
			got = <-done
			guiSide.Close()
		}
		return got
	}
	t.Cleanup(func() { stop() })
	return stop
}

// filesIn returns file name -> content for every file in dir.
func filesIn(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		data, err := os.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		files[e.Name()] = data
	}
	return files
}

// fakeOpenPHM starts a local HTTP server that always answers with status.
// Every request it receives is sent on the returned channel.
func fakeOpenPHM(t *testing.T, status int, reply string) (*httptest.Server, <-chan receivedRequest) {
	t.Helper()
	requests := make(chan receivedRequest, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body model.RequestBody
		err := json.NewDecoder(r.Body).Decode(&body)
		requests <- receivedRequest{method: r.Method, contentType: r.Header.Get("Content-Type"), body: body, decodeErr: err}
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, requests
}

type receivedRequest struct {
	method      string
	contentType string
	body        model.RequestBody
	decodeErr   error
}

// 200: no error, and the data is archived.
func TestSendMeasurements_Server200ArchivesTheData(t *testing.T) {
	useTempCacheDir(t)
	srv, requests := fakeOpenPHM(t, http.StatusOK, "ok")
	stop := listenForBroadcasts(t)
	gateway := &model.Gateway{Id: "gw-1", Password: "secret", HTTPEndpoint: srv.URL}

	resp, err := sendMeasurements(testMeasurementsJSON, gateway)
	if err != nil {
		t.Fatalf("sendMeasurements returned error %v, want nil", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// The server received what the gateway should send: POST, JSON, id, password, measurements.
	select {
	case req := <-requests:
		if req.decodeErr != nil {
			t.Fatalf("server could not decode the request body: %v", req.decodeErr)
		}
		if req.method != http.MethodPost {
			t.Errorf("method = %s, want POST", req.method)
		}
		if req.contentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", req.contentType)
		}
		if req.body.GatewayId != "gw-1" || req.body.GatewayPassword != "secret" {
			t.Errorf("credentials = %q/%q, want gw-1/secret", req.body.GatewayId, req.body.GatewayPassword)
		}
		if len(req.body.Measurements) != 1 || req.body.Measurements[0]["type"] != "vibration" {
			t.Errorf("measurements = %v, want the one vibration measurement", req.body.Measurements)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the fake server never received a request")
	}

	// The data is archived, byte for byte, and was not queued as unsent.
	archived := filesIn(t, archivedDataDir())
	if len(archived) != 1 {
		t.Fatalf("archived %d files, want 1: %v", len(archived), archived)
	}
	for name, content := range archived {
		if !bytes.Equal(content, testMeasurementsJSON) {
			t.Errorf("archived file %s = %s, want %s", name, content, testMeasurementsJSON)
		}
	}
	if unsent := filesIn(t, unsentDataDir()); len(unsent) != 0 {
		t.Errorf("%d files in unsent_data, want 0", len(unsent))
	}

	// The GUI is told the upload worked, and nothing else.
	if msgs := stop(); len(msgs) != 1 || msgs[0] != "UPLOAD-SUCCESS" {
		t.Errorf("broadcasts = %v, want [UPLOAD-SUCCESS]", msgs)
	}
}

// 401: the GATEWAY-INVALID path runs.
func TestSendMeasurements_Server401BroadcastsGatewayInvalid(t *testing.T) {
	useTempCacheDir(t)
	srv, requests := fakeOpenPHM(t, http.StatusUnauthorized, "invalid gateway id or password")
	stop := listenForBroadcasts(t)
	gateway := &model.Gateway{Id: "gw-1", Password: "wrong", HTTPEndpoint: srv.URL}

	resp, err := sendMeasurements(testMeasurementsJSON, gateway)
	if err != nil {
		t.Fatalf("sendMeasurements returned error %v, want nil", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	select {
	case <-requests:
	case <-time.After(2 * time.Second):
		t.Fatal("the fake server never received a request")
	}

	// GATEWAY-INVALID is broadcast, and the upload is not reported as a success.
	msgs := stop()
	if len(msgs) != 1 || msgs[0] != "GATEWAY-INVALID" {
		t.Errorf("broadcasts = %v, want [GATEWAY-INVALID]", msgs)
	}
	// A rejected upload is not archived.
	if archived := filesIn(t, archivedDataDir()); len(archived) != 0 {
		t.Errorf("%d files archived after a 401, want 0", len(archived))
	}
}

// 500: the data isn't treated as uploaded.
func TestSendMeasurements_Server500IsNotTreatedAsUploaded(t *testing.T) {
	useTempCacheDir(t)
	srv, requests := fakeOpenPHM(t, http.StatusInternalServerError, "database is down")
	stop := listenForBroadcasts(t)
	gateway := &model.Gateway{Id: "gw-1", Password: "secret", HTTPEndpoint: srv.URL}

	resp, err := sendMeasurements(testMeasurementsJSON, gateway)
	if err != nil {
		t.Fatalf("sendMeasurements returned error %v, want nil", err)
	}
	defer resp.Body.Close()
	// The caller gets the 500 back, which is how it knows to keep the data.
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	select {
	case <-requests:
	case <-time.After(2 * time.Second):
		t.Fatal("the fake server never received a request")
	}

	// Nothing archived, no UPLOAD-SUCCESS, and it is not mistaken for a bad gateway login.
	if archived := filesIn(t, archivedDataDir()); len(archived) != 0 {
		t.Errorf("%d files archived after a 500, want 0", len(archived))
	}
	if msgs := stop(); len(msgs) != 0 {
		t.Errorf("broadcasts = %v, want none", msgs)
	}
}

// 500 while retrying saved data: the file must stay in unsent_data, not move to sent_data.
func TestSendUnsentMeasurements_Server500KeepsTheFileUnsent(t *testing.T) {
	useTempCacheDir(t)
	srv, _ := fakeOpenPHM(t, http.StatusInternalServerError, "database is down")
	oldGateway := Gateway
	Gateway = &model.Gateway{Id: "gw-1", Password: "secret", HTTPEndpoint: srv.URL}
	t.Cleanup(func() { Gateway = oldGateway })

	if err := saveUnsentMeasurements(testMeasurementsJSON, time.Now()); err != nil {
		t.Fatalf("saveUnsentMeasurements: %v", err)
	}

	sendUnsentMeasurements()

	unsent := filesIn(t, unsentDataDir())
	if len(unsent) != 1 {
		t.Fatalf("%d files left in unsent_data, want 1", len(unsent))
	}
	for _, content := range unsent {
		if !bytes.Equal(content, testMeasurementsJSON) {
			t.Errorf("unsent file changed: %s", content)
		}
	}
	if archived := filesIn(t, archivedDataDir()); len(archived) != 0 {
		t.Errorf("%d files archived after a 500, want 0", len(archived))
	}
}