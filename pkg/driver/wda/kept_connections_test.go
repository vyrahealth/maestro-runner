package wda

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
)

// keptConnServer answers an element's four property reads, each after a short delay so the
// four of a burst are in flight together, as they are on a phone. It counts the connections
// it accepted, and with dropNewAfter > 0 closes, without an answer, every new connection past
// that many: a forward whose new connections fail, while kept ones work.
type keptConnServer struct {
	server       *httptest.Server
	accepted     int32
	dropped      int32
	dropNewAfter int32
}

func newKeptConnServer(t *testing.T, dropNewAfter int32) *keptConnServer {
	t.Helper()
	es := &keptConnServer{dropNewAfter: dropNewAfter}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		switch {
		case strings.HasSuffix(r.URL.Path, "/rect"):
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"x": 10, "y": 20, "width": 100, "height": 40}})
		case strings.HasSuffix(r.URL.Path, "/displayed"):
			jsonResponse(w, map[string]interface{}{"value": true})
		default:
			jsonResponse(w, map[string]interface{}{"value": "Continue"})
		}
	})
	es.server = httptest.NewUnstartedServer(handler)
	es.server.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if state != http.StateNew {
			return
		}
		n := atomic.AddInt32(&es.accepted, 1)
		if es.dropNewAfter > 0 && n > es.dropNewAfter {
			atomic.AddInt32(&es.dropped, 1)
			_ = c.Close()
		}
	}
	es.server.Start()
	t.Cleanup(es.server.Close)
	return es
}

// driverFor is a driver whose client is built the way the runner builds it.
func (es *keptConnServer) driverFor(t *testing.T) *Driver {
	t.Helper()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(es.server.URL, "http://"))
	if err != nil {
		t.Fatalf("server address %q: %v", es.server.URL, err)
	}
	port, _ := strconv.Atoi(portText)
	client := NewClient(uint16(port))
	client.baseURL = es.server.URL // the listener is on 127.0.0.1, not localhost's first address
	client.sessionID = "s1"
	return &Driver{client: client, info: &core.PlatformInfo{Platform: "ios", ScreenWidth: 390, ScreenHeight: 844}}
}

// Fifty elements' reads, four at a time, open four connections and then keep them, instead of
// closing two and opening two for every element.
func TestElementReadsKeepTheirConnections(t *testing.T) {
	es := newKeptConnServer(t, 0)
	d := es.driverFor(t)
	for i := 0; i < 50; i++ {
		if _, err := d.getElementInfo(fmt.Sprintf("E%d", i)); err != nil {
			t.Fatalf("element %d: %v", i, err)
		}
	}
	if n := atomic.LoadInt32(&es.accepted); n > 4 {
		t.Errorf("50 bursts of four reads opened %d connections, want at most 4", n)
	}
}

// Where a new connection fails (the phone's forward), only the first burst opens any, so the
// reads after it never meet a failing one.
func TestElementReadsSurviveAForwardWhoseNewConnectionsFail(t *testing.T) {
	es := newKeptConnServer(t, 4)
	d := es.driverFor(t)
	var mu sync.Mutex
	failed := 0
	for i := 0; i < 50; i++ {
		if _, err := d.getElementInfo(fmt.Sprintf("E%d", i)); err != nil {
			mu.Lock()
			failed++
			mu.Unlock()
		}
	}
	if dropped := atomic.LoadInt32(&es.dropped); dropped != 0 || failed != 0 {
		t.Errorf("after the first burst, %d new connections were needed and dropped, and %d reads failed; want none", dropped, failed)
	}
}

func TestTheWDATransportKeepsABurstsConnections(t *testing.T) {
	tr := newWDATransport()
	if tr.MaxIdleConnsPerHost < 4 {
		t.Errorf("MaxIdleConnsPerHost = %d, fewer than the driver's four requests at once", tr.MaxIdleConnsPerHost)
	}
	if tr.Proxy == nil || tr.DialContext == nil {
		t.Error("the WDA transport lost the default transport's proxy or dialer")
	}
}
