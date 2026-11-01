package server

import (
	"testing"

	"github.com/jukuly/ss_machmos/server/internal/model"
)

func TestSendMeasurements(t *testing.T) {
	t.Skip("known bug: issue #6")

	_, _ = sendMeasurements([]byte("[]"), &model.Gateway{HTTPEndpoint: "http://127.0.0.1:1"})
}
