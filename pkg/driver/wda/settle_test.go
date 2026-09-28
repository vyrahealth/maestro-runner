package wda

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The executor drives Maestro's iOS tap through this method (tap_options.go).
var _ interface{ WaitUntilScreenIsStatic(int) bool } = (*Driver)(nil)

// frameServer is a fake WDA whose screenshots play frames in turn, the last one
// repeating; an empty frame is a failed screenshot. With moving set, every
// screenshot differs from the last. It counts screenshots.
type frameServer struct {
	mu     sync.Mutex
	frames []string
	moving bool
	shots  int
}

func (f *frameServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/screenshot") {
			jsonResponse(w, map[string]interface{}{"value": nil})
			return
		}
		f.mu.Lock()
		i := f.shots
		f.shots++
		f.mu.Unlock()
		frame := fmt.Sprintf("frame-%d", i)
		if !f.moving {
			frame = f.frames[min(i, len(f.frames)-1)]
		}
		if frame == "" {
			w.WriteHeader(http.StatusInternalServerError)
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"error": "unknown error", "message": "cannot take a screenshot"}})
			return
		}
		jsonResponse(w, map[string]interface{}{"value": base64.StdEncoding.EncodeToString([]byte(frame))})
	}))
	t.Cleanup(server.Close)
	return server
}

func (f *frameServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shots
}

func TestWaitUntilScreenIsStatic(t *testing.T) {
	for _, tc := range []struct {
		name      string
		frames    []string
		want      bool
		wantShots int
	}{
		{"a still screen costs two screenshots", []string{"A"}, true, 2},
		{"an animation ends", []string{"A", "B", "C", "C"}, true, 4},
		// Any difference is movement: Maestro compares hashes, not a tolerance.
		{"one byte differs", []string{"frame-1", "frame-2", "frame-2"}, true, 3},
		{"a failed screenshot ends the wait", []string{"A", ""}, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := &frameServer{frames: tc.frames}
			d := createTestDriver(fs.start(t))
			if got := d.WaitUntilScreenIsStatic(3000); got != tc.want {
				t.Errorf("static = %v, want %v", got, tc.want)
			}
			if got := fs.count(); got != tc.wantShots {
				t.Errorf("screenshots = %d, want %d", got, tc.wantShots)
			}
		})
	}
}

// A screen that never holds still is given up on at the limit.
func TestWaitUntilScreenIsStaticGivesUpAtTheLimit(t *testing.T) {
	fs := &frameServer{moving: true}
	d := createTestDriver(fs.start(t))
	start := time.Now()
	if d.WaitUntilScreenIsStatic(300) {
		t.Fatal("a screen that keeps changing is not static")
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond || elapsed > 2*time.Second {
		t.Errorf("gave up after %v, want about 300ms", elapsed)
	}
}
