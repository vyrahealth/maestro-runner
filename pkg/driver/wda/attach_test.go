package wda

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAttachExistingReadsTheSwitch(t *testing.T) {
	t.Setenv("MAESTRO_WDA_ATTACH", "")
	if AttachExisting() {
		t.Fatal("an empty MAESTRO_WDA_ATTACH must leave the runner starting its own WDA")
	}
	t.Setenv("MAESTRO_WDA_ATTACH", "1")
	if !AttachExisting() {
		t.Fatal("MAESTRO_WDA_ATTACH=1 must attach")
	}
}

func shortPolls(t *testing.T) {
	t.Helper()
	old := attachPollInterval
	attachPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { attachPollInterval = old })
}

// A WDA that was just started refuses the first probes; the wait keeps asking until it answers.
func TestWaitForRunningWaitsForAWDAThatIsStillStarting(t *testing.T) {
	shortPolls(t)
	var asked atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("asked %s, not /status", r.URL.Path)
		}
		if asked.Add(1) < 3 {
			http.Error(w, `{"value":{"error":"starting"}}`, http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"value":{"ready":true,"state":"success"},"sessionId":null}`))
	}))
	defer server.Close()
	c := NewClient(8100)
	c.baseURL = server.URL

	if err := WaitForRunning(c, 5*time.Second); err != nil {
		t.Fatalf("WaitForRunning: %v", err)
	}
	if n := asked.Load(); n != 3 {
		t.Fatalf("asked /status %d times, want 3 (two refusals, then the answer)", n)
	}
}

// Nothing listening: the wait gives up after its timeout and says where it looked.
func TestWaitForRunningGivesUpWhenNoWDAAnswers(t *testing.T) {
	shortPolls(t)
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close() // the port is now closed

	c := NewClient(8100)
	c.baseURL = url
	start := time.Now()
	err := WaitForRunning(c, 200*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForRunning passed with nothing listening")
	}
	if !strings.Contains(err.Error(), url) {
		t.Fatalf("the error does not name where it looked (%s): %v", url, err)
	}
	if took := time.Since(start); took < 200*time.Millisecond || took > 3*time.Second {
		t.Fatalf("gave up after %v, want about the 200ms timeout", took)
	}
}
