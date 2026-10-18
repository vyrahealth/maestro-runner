package appium

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// cropOn crops the screenshot to the matched element; it used to be ignored,
// so assertScreenshot compared the whole screen against a cropped baseline.
func TestTakeScreenshotCropsToTheElement(t *testing.T) {
	var shot bytes.Buffer
	_ = png.Encode(&shot, image.NewRGBA(image.Rect(0, 0, 200, 400)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch p := r.URL.Path; {
		case strings.HasSuffix(p, "/screenshot"):
			writeJSON(w, map[string]interface{}{"value": base64.StdEncoding.EncodeToString(shot.Bytes())})
		case strings.HasSuffix(p, "/element") && r.Method == http.MethodPost:
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{w3cElementKey: "logo"}})
		case strings.HasSuffix(p, "/rect"):
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{"x": 20, "y": 40, "width": 60, "height": 30}})
		case strings.HasSuffix(p, "/displayed"):
			writeJSON(w, map[string]interface{}{"value": true})
		default:
			writeJSON(w, map[string]interface{}{"value": ""})
		}
	}))
	t.Cleanup(server.Close)
	d := createTestAppiumDriver(server)
	d.client.screenW, d.client.screenH = 200, 400

	res := d.takeScreenshot(&flow.TakeScreenshotStep{CropOn: &flow.Selector{ID: "logo"}})
	if !res.Success {
		t.Fatalf("takeScreenshot: %s", res.Message)
	}
	img, err := png.Decode(bytes.NewReader(res.Data.([]byte)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 60 || b.Dy() != 30 {
		t.Errorf("image is %dx%d, want the element's 60x30", b.Dx(), b.Dy())
	}
}
