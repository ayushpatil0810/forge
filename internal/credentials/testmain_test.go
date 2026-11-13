package credentials

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	ArmForTest()
	os.Exit(m.Run())
}

func TestNetGuardRejectsNonLoopback(t *testing.T) {
	_, err := http.Get("http://example.com/")
	if err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("expected non-loopback dial to be refused, got %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("loopback dial was refused: %v", err)
	}
	_ = resp.Body.Close()
}
