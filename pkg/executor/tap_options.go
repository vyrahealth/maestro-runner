package executor

import (
	"bytes"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
	"github.com/devicelab-dev/maestro-runner/pkg/logger"
)

// tapOptions holds tap-related execution options extracted from a step.
type tapOptions struct {
	Repeat                int   // Number of times to execute the tap (0 or 1 = once)
	DelayMs               int   // Delay between repeated taps in ms
	RetryTapIfNoChange    *bool // If true, retry tap if hierarchy unchanged
	WaitToSettleTimeoutMs int   // Wait for UI to settle before/after tap (ms)
}

// extractTapOptions extracts tap options from a tap-type step.
// Returns false for non-tap steps.
func extractTapOptions(step flow.Step) (tapOptions, bool) {
	switch s := step.(type) {
	case *flow.TapOnStep:
		return tapOptions{
			Repeat:                s.Repeat,
			DelayMs:               s.DelayMs,
			RetryTapIfNoChange:    s.RetryTapIfNoChange,
			WaitToSettleTimeoutMs: s.WaitToSettleTimeoutMs,
		}, true
	case *flow.DoubleTapOnStep:
		return tapOptions{
			RetryTapIfNoChange:    s.RetryTapIfNoChange,
			WaitToSettleTimeoutMs: s.WaitToSettleTimeoutMs,
		}, true
	case *flow.LongPressOnStep:
		return tapOptions{
			RetryTapIfNoChange:    s.RetryTapIfNoChange,
			WaitToSettleTimeoutMs: s.WaitToSettleTimeoutMs,
		}, true
	default:
		return tapOptions{}, false
	}
}

// hasTapOptions returns true if any non-default options are set.
func (opts tapOptions) hasTapOptions() bool {
	return opts.Repeat > 1 || opts.DelayMs > 0 ||
		opts.RetryTapIfNoChange != nil || opts.WaitToSettleTimeoutMs > 0
}

const (
	defaultRepeatDelay = 100 // ms, matches Maestro's DEFAULT_REPEAT_DELAY
	settleInterval     = 200 * time.Millisecond
)

// settleAfterAction waits for the UI to settle after a UI-mutating action.
// Matches Maestro's behavior of calling waitForAppToSettle() after every action.
// Uses DeviceLab's native event-based settle when available, falls back to hierarchy comparison.
//
//nolint:unused
func (fr *FlowRunner) settleAfterAction() {
	// Check if driver supports native settle (DeviceLab)
	if settler, ok := core.Unwrap(fr.driver).(interface {
		WaitForSettle(timeoutMs, quietMs int) (bool, error)
	}); ok {
		settled, err := settler.WaitForSettle(2000, 150)
		if err != nil {
			logger.Warn("settleAfterAction: native settle error: %v", err)
		} else if !settled {
			logger.Debug("settleAfterAction: native settle timed out")
		}
		return
	}

	// Fallback: hierarchy comparison (10 polls × 200ms, same as Maestro default)
	fr.waitForSettle(2000)
}

// isTapAction returns true if the step is a tap that may trigger a screen transition.
//
//nolint:unused
func isTapAction(step flow.Step) bool {
	switch step.(type) {
	case *flow.TapOnStep, *flow.DoubleTapOnStep, *flow.LongPressOnStep, *flow.TapOnPointStep:
		return true
	default:
		return false
	}
}

// needsPreSettle returns true if the step needs the UI to be settled before executing.
// These steps don't call findElement (which has implicit idle wait), so they need
// explicit settle to avoid timing issues after screen transitions.
//
//nolint:unused
func needsPreSettle(step flow.Step) bool {
	switch step.(type) {
	case *flow.InputTextStep, *flow.InputRandomStep, *flow.EraseTextStep:
		return true
	default:
		return false
	}
}

// waitForSettle polls Hierarchy() until two consecutive snapshots match,
// or the timeout is reached. Returns the final hierarchy snapshot.
// If timeoutMs <= 0, returns the current hierarchy without polling.
func (fr *FlowRunner) waitForSettle(timeoutMs int) []byte {
	hierarchy, err := fr.driver.Hierarchy()
	if err != nil {
		logger.Debug("waitForSettle: Hierarchy() error: %v", err)
		return nil
	}
	if timeoutMs <= 0 {
		return hierarchy
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(settleInterval)
		next, err := fr.driver.Hierarchy()
		if err != nil {
			logger.Debug("waitForSettle: Hierarchy() error: %v", err)
			return hierarchy
		}
		if bytes.Equal(hierarchy, next) {
			return next
		}
		hierarchy = next
	}
	return hierarchy
}

// executeTapWithOptions wraps a tap step with repeat, delay,
// retryTapIfNoChange, and waitToSettleTimeoutMs logic.
//
// Execution order (matching Maestro):
//  1. hierarchyBefore = waitForSettle(waitToSettleTimeoutMs)
//  2. retryLoop (retryTapIfNoChange ? 2 : 1):
//     a. repeatLoop with delay between taps
//     b. hierarchyAfter = waitForSettle(waitToSettleTimeoutMs)
//     c. if hierarchy changed → return
//  3. return last result
//
// A driver that settles the way Maestro's iOS driver does gets Maestro's iOS
// tap instead (executeScreenshotBasedTap).
func (fr *FlowRunner) executeTapWithOptions(step flow.Step, opts tapOptions) *core.CommandResult {
	if !opts.hasTapOptions() {
		return fr.driver.Execute(step)
	}
	if settler, ok := core.Unwrap(fr.driver).(screenshotSettler); ok {
		return fr.executeScreenshotBasedTap(step, opts, settler)
	}

	settleTimeout := opts.WaitToSettleTimeoutMs

	// Capture hierarchy before tap (for settle and/or retry comparison)
	var hierarchyBefore []byte
	if settleTimeout > 0 || opts.RetryTapIfNoChange != nil {
		hierarchyBefore = fr.waitForSettle(settleTimeout)
	}

	// Retry count: 2 if retryTapIfNoChange, else 1
	retryCount := 1
	if opts.RetryTapIfNoChange != nil && *opts.RetryTapIfNoChange {
		retryCount = 2
	}

	var lastResult *core.CommandResult

	for attempt := 0; attempt < retryCount; attempt++ {
		if fr.ctx.Err() != nil {
			return &core.CommandResult{
				Success: false,
				Error:   fr.ctx.Err(),
				Message: "Tap cancelled",
			}
		}

		// Execute tap (possibly repeated)
		lastResult = fr.repeatTap(step, opts)
		if !lastResult.Success {
			return lastResult
		}

		// Check if UI changed (for retry and settle logic)
		if settleTimeout > 0 || opts.RetryTapIfNoChange != nil {
			hierarchyAfter := fr.waitForSettle(settleTimeout)
			if hierarchyBefore != nil && hierarchyAfter != nil &&
				!bytes.Equal(hierarchyBefore, hierarchyAfter) {
				logger.Debug("Tap caused UI change (attempt %d)", attempt+1)
				return lastResult
			}
			if attempt < retryCount-1 {
				logger.Debug("Tap had no UI change, retrying (attempt %d/%d)", attempt+1, retryCount)
			}
		}
	}

	return lastResult
}

// repeatTap executes the tap the step's repeat count of times, delay apart,
// and stops at the first that fails.
func (fr *FlowRunner) repeatTap(step flow.Step, opts tapOptions) *core.CommandResult {
	repeatCount := opts.Repeat
	if repeatCount <= 0 {
		repeatCount = 1
	}

	delayMs := opts.DelayMs
	if delayMs <= 0 && repeatCount > 1 {
		delayMs = defaultRepeatDelay
	}

	var lastResult *core.CommandResult
	for i := 0; i < repeatCount; i++ {
		tapStart := time.Now()
		lastResult = fr.driver.Execute(step)
		if !lastResult.Success {
			return lastResult
		}

		// Delay between repeated taps (not after the last one)
		if repeatCount > 1 && i < repeatCount-1 {
			sleepTime := time.Duration(delayMs)*time.Millisecond - time.Since(tapStart)
			if sleepTime > 0 {
				time.Sleep(sleepTime)
			}
		}
	}
	return lastResult
}

// screenshotSettler is a driver that waits for the screen to hold still the
// way Maestro's iOS driver does, until two screenshots in a row are the same
// (IOSDriver.kt:490-507), and reports whether that happened in time.
type screenshotSettler interface {
	WaitUntilScreenIsStatic(timeoutMs int) bool
}

const (
	// screenStaticTimeoutMs is how long Maestro's iOS driver lets the screen
	// take to hold still (IOSDriver.kt:692).
	screenStaticTimeoutMs = 3000
	// screenChangeThreshold is Maestro's SCREENSHOT_DIFF_THRESHOLD
	// (Maestro.kt:783), in the percent core.ScreenChangePercent reports.
	screenChangeThreshold = 0.005
	// hierarchySettleMs stands in for Maestro's hierarchy settle when the flow
	// sets no waitToSettleTimeoutMs: ten reads 200 ms apart
	// (ScreenshotUtils.kt:57-71).
	hierarchySettleMs = 2000
)

// executeScreenshotBasedTap is Maestro's tap on iOS (screenshotBasedTap,
// Maestro.kt:438-502). After the tap the screen gets up to 3 s to hold still,
// and when it does the tap counts as having changed something: Maestro's
// settle then returns no hierarchy, which never equals the one from before the
// tap (IOSDriver.kt:500-507, Maestro.kt:470-475). So retryTapIfNoChange only
// taps again when the screen never held still and neither the hierarchy nor
// the screenshot changed, and waitToSettleTimeoutMs only bounds the hierarchy
// settle after those 3 s (ScreenshotUtils.kt:38-74). Without
// retryTapIfNoChange: true (Maestro's default is false, YamlFluentCommand.kt:802
// and Orchestra.kt:393) the tap costs nothing more than itself.
func (fr *FlowRunner) executeScreenshotBasedTap(step flow.Step, opts tapOptions, settler screenshotSettler) *core.CommandResult {
	if opts.RetryTapIfNoChange == nil || !*opts.RetryTapIfNoChange {
		return fr.repeatTap(step, opts)
	}

	hierarchyBefore, err := fr.driver.Hierarchy()
	if err != nil {
		logger.Debug("retryTapIfNoChange: hierarchy before the tap: %v", err)
	}
	screenshotBefore, _ := fr.driver.Screenshot()
	settleTimeout := opts.WaitToSettleTimeoutMs
	if settleTimeout <= 0 {
		settleTimeout = hierarchySettleMs
	}

	var lastResult *core.CommandResult
	for attempt := 1; attempt <= 2; attempt++ {
		if fr.ctx.Err() != nil {
			return &core.CommandResult{
				Success: false,
				Error:   fr.ctx.Err(),
				Message: "Tap cancelled",
			}
		}

		lastResult = fr.repeatTap(step, opts)
		if !lastResult.Success {
			return lastResult
		}
		if settler.WaitUntilScreenIsStatic(screenStaticTimeoutMs) {
			return lastResult
		}

		// A hierarchy that cannot be read proves nothing, and a second tap
		// on a tap that worked is worse than none.
		hierarchyAfter := fr.waitForSettle(settleTimeout)
		if hierarchyBefore == nil || hierarchyAfter == nil || !bytes.Equal(hierarchyBefore, hierarchyAfter) {
			logger.Debug("Tap caused UI change (attempt %d)", attempt)
			return lastResult
		}
		// Screenshots that cannot be compared are no evidence of a change,
		// as in Maestro.
		if screenshotAfter, err := fr.driver.Screenshot(); err == nil {
			if diff, ok := core.ScreenChangePercent(screenshotBefore, screenshotAfter); ok && diff > screenChangeThreshold {
				logger.Debug("Tap changed the screenshot (%.4f%%, attempt %d)", diff, attempt)
				return lastResult
			}
		}
		if attempt == 1 {
			logger.Debug("Tap had no UI change, retrying (attempt %d/2)", attempt)
		}
	}
	return lastResult
}
