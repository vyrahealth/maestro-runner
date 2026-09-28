package wda

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// droppingServer closes the connection, with no response, on the first request to
// each path in drop; every later request is answered.
func droppingServer(t *testing.T, drop ...string) (*httptest.Server, func(string) int) {
	t.Helper()
	var mu sync.Mutex
	seen := map[string]int{}
	dropFirst := map[string]bool{}
	for _, p := range drop {
		dropFirst[p] = true
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path]++
		n := seen[r.URL.Path]
		mu.Unlock()
		if dropFirst[r.URL.Path] && n == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/text"):
			jsonResponse(w, map[string]interface{}{"value": "72.4"})
		case strings.HasSuffix(r.URL.Path, "/elements"):
			jsonResponse(w, map[string]interface{}{"value": []interface{}{map[string]interface{}{"ELEMENT": "e1"}}})
		default:
			jsonResponse(w, map[string]interface{}{"value": nil})
		}
	}))
	count := func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return seen[path]
	}
	return server, count
}

func testClient(server *httptest.Server) *Client {
	return &Client{baseURL: server.URL, httpClient: http.DefaultClient, sessionID: "s"}
}

// The measured failure: a text read whose connection dropped left copyTextFrom
// with an empty string. The read is now sent once more and gets its answer.
func TestGetIsSentAgainWhenItsConnectionDrops(t *testing.T) {
	server, count := droppingServer(t, "/session/s/element/e1/text")
	defer server.Close()
	text, err := testClient(server).ElementText("e1")
	if err != nil || text != "72.4" {
		t.Fatalf("got %q, %v; want the text after one more try", text, err)
	}
	if n := count("/session/s/element/e1/text"); n != 2 {
		t.Errorf("WDA saw the read %d times, want 2", n)
	}
}

func TestLookupIsSentAgainWhenItsConnectionDrops(t *testing.T) {
	server, count := droppingServer(t, "/session/s/elements")
	defer server.Close()
	ids, err := testClient(server).FindElements("class chain", "**/XCUIElementTypeAny")
	if err != nil || len(ids) != 1 {
		t.Fatalf("got %v, %v; want the element after one more try", ids, err)
	}
	if n := count("/session/s/elements"); n != 2 {
		t.Errorf("WDA saw the lookup %d times, want 2", n)
	}
}

// A tap may have reached WDA before the connection went, so it is never repeated:
// the error comes back and WDA saw it exactly once.
func TestActionIsNotSentAgainWhenItsConnectionDrops(t *testing.T) {
	server, count := droppingServer(t, "/session/s/element/e1/click")
	defer server.Close()
	if err := testClient(server).ElementClick("e1"); err == nil {
		t.Fatal("the tap reported success over a dropped connection")
	}
	if n := count("/session/s/element/e1/click"); n != 1 {
		t.Errorf("WDA saw the tap %d times, want 1", n)
	}
}

// A request that fails twice fails: one more try, not a loop.
func TestGetIsTriedOnlyOnceMore(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	}))
	defer server.Close()
	if _, err := testClient(server).ElementText("e1"); err == nil {
		t.Fatal("a read that dropped twice reported success")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 2 {
		t.Errorf("WDA saw the read %d times, want 2", hits)
	}
}
