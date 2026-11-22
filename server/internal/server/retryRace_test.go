package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/jukuly/ss_machmos/server/internal/model"
)

// retryRaceUploadCounter is a fake OpenPHM server that counts how many times
// each measurement file was uploaded. Every test file carries a unique "marker"
// value so the server can tell the files apart.
type retryRaceUploadCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *retryRaceUploadCounter) countFor(marker string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[marker]
}

// The 5-minute retry loop and a normal upload can both call
// sendUnsentMeasurements() at the same moment. Each saved file must still be
// uploaded exactly once, not once per caller.
func TestSendUnsentMeasurements_TwoCallsAtOnceUploadEachFileOnce(t *testing.T) {
	const fileCount = 5

	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // empty cache folder for this test

	// Fake OpenPHM server: counts uploads per file. The short sleep keeps both
	// callers busy at the same time, which is what happens with a real network.
	counter := &retryRaceUploadCounter{counts: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body model.RequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Measurements) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		marker, _ := body.Measurements[0]["marker"].(string)

		time.Sleep(30 * time.Millisecond)

		counter.mu.Lock()
		counter.counts[marker]++
		counter.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	oldGateway := Gateway
	Gateway = &model.Gateway{Id: "gw-1", Password: "secret", HTTPEndpoint: srv.URL}
	t.Cleanup(func() { Gateway = oldGateway })

	// Five saved measurement files waiting in unsent_data.
	for i := 0; i < fileCount; i++ {
		name := fmt.Sprintf("retry-race-%d.json", i)
		content := fmt.Sprintf(`[{"marker":"retry-race-%d"}]`, i)
		if err := os.WriteFile(path.Join(unsentDataDir(), name), []byte(content), 0644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	// Two callers, released at the same instant.
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			sendUnsentMeasurements()
		}()
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()

	close(start)
	select {
	case <-finished:
	case <-time.After(15 * time.Second):
		t.Fatal("sendUnsentMeasurements() did not finish within 15 s (deadlock?)")
	}

	// Each file must have reached the server exactly once.
	for i := 0; i < fileCount; i++ {
		marker := fmt.Sprintf("retry-race-%d", i)
		if got := counter.countFor(marker); got != 1 {
			t.Errorf("file %s was uploaded %d times, want exactly 1", marker, got)
		}
	}

	// Everything ended up in sent_data and nothing is left to retry.
	if left, err := os.ReadDir(unsentDataDir()); err != nil || len(left) != 0 {
		t.Errorf("unsent_data still holds %d entries (err %v), want 0", len(left), err)
	}
	for i := 0; i < fileCount; i++ {
		name := fmt.Sprintf("retry-race-%d.json", i)
		if _, err := os.Stat(path.Join(archivedDataDir(), name)); err != nil {
			t.Errorf("%s is not in sent_data: %v", name, err)
		}
	}
}