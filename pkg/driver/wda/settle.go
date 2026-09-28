package wda

import (
	"bytes"
	"os"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
	"github.com/devicelab-dev/maestro-runner/pkg/logger"
)

// WaitUntilScreenIsStatic waits up to timeoutMs for two screenshots in a row
// to be the same, and reports whether they were. It is how Maestro waits for
// the screen on iOS (IOSDriver.kt:490-507): its runner compares the SHA-256 of
// two screenshots (ScreenDiffHandler.swift:16-21), so any difference at all is
// a screen still changing, and the bytes are compared here the same way. Each
// screenshot is compared with the one before it, so a still screen costs two.
// A screenshot that fails ends the wait, since nothing more can be learned.
// Each wait logs what it took, so a device whose screenshots never repeat
// byte for byte shows up as waits that always run to the limit.
func (d *Driver) WaitUntilScreenIsStatic(timeoutMs int) bool {
	start := time.Now()
	deadline := start.Add(time.Duration(timeoutMs) * time.Millisecond)
	prev, err := d.client.Screenshot()
	shots := 1
	for err == nil && time.Now().Before(deadline) {
		var next []byte
		next, err = d.client.Screenshot()
		shots++
		if err == nil && len(next) > 0 && bytes.Equal(prev, next) {
			logger.Debug("[wda] screen still after %d screenshots (%dms)", shots, time.Since(start).Milliseconds())
			return true
		}
		prev = next
	}
	if err != nil {
		logger.Debug("[wda] screen settle: screenshot failed: %v", err)
	} else {
		logger.Debug("[wda] screen still changing after %d screenshots (%dms)", shots, time.Since(start).Milliseconds())
	}
	return false
}

// screenSettleLimitMs is how long the screen gets to hold still with
// MAESTRO_WDA_SETTLE set (IOSDriver.kt:692). A variable so tests can shorten
// it.
var screenSettleLimitMs = 3000

// A tapped element gets this long to stop moving, read this often
// (Maestro.kt:788-789).
const (
	elementStableLimit  = 3 * time.Second
	elementStableSample = 100 * time.Millisecond
)

// settleOn reports whether MAESTRO_WDA_SETTLE is set. The driver then waits
// for the screen the way Maestro does on iOS, where it is Maestro's only wait:
// XCTest's wait for quiescence is off on both sides.
func settleOn() bool {
	return os.Getenv("MAESTRO_WDA_SETTLE") != ""
}

// settleBefore waits for the screen to hold still before a swipe or a scroll,
// as Maestro's iOS driver does before every swipe (IOSDriver.kt:269). Element
// taps wait after their lookup instead (settleBeforeTap).
func (d *Driver) settleBefore(step flow.Step) {
	if !settleOn() {
		return
	}
	switch step.(type) {
	case *flow.SwipeStep, *flow.ScrollStep:
		d.WaitUntilScreenIsStatic(screenSettleLimitMs)
	}
}

// settleAfter waits for the screen to hold still after a step that changes
// it, as Maestro does after every tap, swipe, scroll, inputText, eraseText,
// pressKey, back, openLink and setOrientation (Maestro.kt:129, 182, 195, 205,
// 213, 470, 525, 585, 592, 722; Orchestra.kt:1254). hideKeyboard presses a key
// here, so it settles as pressKey does, and dragAndDrop, which Maestro lacks,
// as a swipe does. It also keeps track of the swipe or scroll the next element
// tap must let come to rest: set by one, cleared by launchApp and by the next
// tap that happens (Maestro.kt:88, 181-212, 387), so not by an optional tapOn
// that found nothing.
func (d *Driver) settleAfter(step flow.Step, result *core.CommandResult) {
	switch s := step.(type) {
	case *flow.SwipeStep, *flow.ScrollStep, *flow.ScrollUntilVisibleStep:
		if result.Success {
			d.recentScroll = true
		}
	case *flow.LaunchAppStep:
		d.recentScroll = false
	case *flow.TapOnStep:
		pointTap := s.Point != "" && s.Selector.IsEmpty()
		if result.Success && (result.Element != nil || pointTap) {
			d.recentScroll = false
		}
	case *flow.DoubleTapOnStep, *flow.LongPressOnStep, *flow.TapOnPointStep:
		if result.Success {
			d.recentScroll = false
		}
	}
	if !result.Success || !settleOn() {
		return
	}
	switch step.(type) {
	case *flow.TapOnStep, *flow.DoubleTapOnStep, *flow.LongPressOnStep, *flow.TapOnPointStep,
		*flow.SwipeStep, *flow.ScrollStep, *flow.ScrollUntilVisibleStep, *flow.DragAndDropStep,
		*flow.InputTextStep, *flow.InputRandomStep, *flow.PasteTextStep, *flow.EraseTextStep,
		*flow.PressKeyStep, *flow.HideKeyboardStep, *flow.BackStep, *flow.OpenLinkStep,
		*flow.SetOrientationStep:
		d.WaitUntilScreenIsStatic(screenSettleLimitMs)
	}
}

// settleBeforeTap waits, with MAESTRO_WDA_SETTLE set, for the screen to hold
// still between an element's lookup and the tap on it, as Maestro does before
// every element tap (Maestro.kt:228). Momentum can outlast that check, so after
// a swipe or scroll, and when the screen never held still, it also waits for
// the element itself to stop moving and moves the tap to where it came to rest
// (Maestro.kt:230-242).
func (d *Driver) settleBeforeTap(sel flow.Selector, info *core.ElementInfo) *core.ElementInfo {
	if !settleOn() || info == nil {
		return info
	}
	if d.WaitUntilScreenIsStatic(screenSettleLimitMs) && !d.recentScroll {
		return info
	}
	info.Bounds = d.restingBounds(sel, info)
	return info
}

// restingBounds reads the element's bounds about every 100 ms until two reads
// in a row agree, for at most 3 s, and returns the last it read
// (Maestro.kt:290-333). An element with a WDA id costs a rect read each time;
// one found in the page source costs a page source. The lookup's bounds count
// as the first read: the screen settle came between the two, which puts them
// further apart than Maestro's two fresh hierarchy reads.
func (d *Driver) restingBounds(sel flow.Selector, info *core.ElementInfo) core.Bounds {
	last := info.Bounds
	deadline := time.Now().Add(elementStableLimit)
	for {
		if b, ok := d.readBounds(sel, info.ID); ok {
			if b == last {
				return b
			}
			last = b
		}
		if !time.Now().Add(elementStableSample).Before(deadline) {
			logger.Debug("[wda] %s still moving after %v: tapping where it was last seen", selectorLog(sel), elementStableLimit)
			return last
		}
		time.Sleep(elementStableSample)
	}
}

// readBounds reads the element's bounds again the way its lookup found them:
// through its WDA id when it has one, else from a page source.
func (d *Driver) readBounds(sel flow.Selector, id string) (core.Bounds, bool) {
	if id != "" {
		x, y, w, h, err := d.client.ElementRect(id)
		return core.Bounds{X: x, Y: y, Width: w, Height: h}, err == nil && w > 0 && h > 0
	}
	var found *core.ElementInfo
	var err error
	if sel.HasRelativeSelector() {
		found, err = d.findElementRelativeOnce(sel)
	} else {
		found, err = d.findElementByPageSourceOnce(sel)
	}
	if err != nil || found == nil {
		return core.Bounds{}, false
	}
	return found.Bounds, true
}
