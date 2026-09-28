package jsengine

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPInsecure verifies that runScript's http.* honours TLS-skip: a
// self-signed TLS server fails verification by default, succeeds with a
// per-request insecure option, and succeeds with the engine-wide default.
func TestHTTPInsecure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	t.Run("verified request fails on a self-signed cert", func(t *testing.T) {
		e := New()
		defer e.Close()
		v, err := e.Eval(`(function(){ try { return http.get(` + jsString(srv.URL) + `).status; } catch (e) { return -1; } })()`)
		// Either the script caught the TLS error (returns -1) or Eval surfaced
		// it — both mean verification was NOT skipped. A 200 would be the bug.
		if err == nil {
			if n, _ := v.(int64); n == 200 {
				t.Error("a verified request must fail on a self-signed certificate")
			}
		}
	})

	t.Run("per-request insecure succeeds", func(t *testing.T) {
		e := New()
		defer e.Close()
		v, err := e.Eval(`http.get(` + jsString(srv.URL) + `, { insecure: true }).status`)
		if err != nil {
			t.Fatalf("eval error: %v", err)
		}
		if n, _ := v.(int64); n != 200 {
			t.Errorf("status = %v, want 200", v)
		}
	})

	t.Run("engine-wide insecure default succeeds", func(t *testing.T) {
		e := New()
		defer e.Close()
		e.SetInsecureHTTP(true)
		v, err := e.Eval(`http.get(` + jsString(srv.URL) + `).status`)
		if err != nil {
			t.Fatalf("eval error: %v", err)
		}
		if n, _ := v.(int64); n != 200 {
			t.Errorf("status = %v, want 200", v)
		}
	})
}

// jsString returns a JS string literal for s.
func jsString(s string) string {
	return "\"" + s + "\""
}
