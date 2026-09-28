package jsengine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Maestro's script client allows a call 5 minutes (GraalJsEngine.kt:28-34).
// The runner's gave up after 30 s, so a slow test-data endpoint that works
// under Maestro failed the script.
func TestDefaultHTTPTimeoutIsMaestros(t *testing.T) {
	if defaultHTTPTimeout != 5*time.Minute {
		t.Errorf("default http timeout = %v, want 5m", defaultHTTPTimeout)
	}
}

// A call with no timeout option waits for the default, and the option still
// wins over it.
func TestHTTPRequestWithoutATimeoutUsesTheDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	saved := defaultHTTPTimeout
	defaultHTTPTimeout = 50 * time.Millisecond
	defer func() { defaultHTTPTimeout = saved }()

	e := New()
	defer e.Close()

	_, err := e.Eval(`http.get(` + jsString(srv.URL) + `).status`)
	if err == nil || !strings.Contains(err.Error(), "HTTP request failed") {
		t.Errorf("a call slower than the default timeout returned err = %v, want it to time out", err)
	}

	v, err := e.Eval(`http.get(` + jsString(srv.URL) + `, { timeout: 5000 }).status`)
	if err != nil {
		t.Fatalf("a call with its own longer timeout failed: %v", err)
	}
	if n, _ := v.(int64); n != 200 {
		t.Errorf("status = %v, want 200", v)
	}
}
