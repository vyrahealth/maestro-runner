package executor

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
	"github.com/devicelab-dev/maestro-runner/pkg/logger"
	"github.com/devicelab-dev/maestro-runner/pkg/report"
)

// maxSystemAlertDismissals caps the system alerts one flow dismisses. An
// alert that keeps coming back needs a person to look at it, so after that
// many the flow stops looking and its failures stand.
const maxSystemAlertDismissals = 3

// checkSystemAlert asks the driver, when it can dismiss system alerts
// (core.SystemAlertDismisser) and the flow has dismissals left, to look for
// one once. A dismissal is a console warning and goes in the report, on the
// flow and on top-level command cmdIndex (-1 for none); step names the step
// it was dismissed for ("" at the flow's start). It returns the alert found,
// dismissed or not, or nil, and the report's record of a dismissal.
func (fr *FlowRunner) checkSystemAlert(step string, cmdIndex int) (*core.SystemAlert, *report.SystemAlert) {
	if fr.systemAlertDismissals >= maxSystemAlertDismissals {
		return nil, nil
	}
	dismisser, ok := core.Unwrap(fr.driver).(core.SystemAlertDismisser)
	if !ok {
		return nil, nil
	}
	alert, err := dismisser.DismissSystemAlert()
	if err != nil {
		logger.Warn("system alert check: %v", err)
	}
	if alert == nil {
		return nil, nil
	}
	if alert.Dismissed == "" {
		logger.Warn("a system alert is covering the app and was left alone: %q, buttons %q", alert.Title, alert.Buttons)
		return alert, nil
	}

	fr.systemAlertDismissals++
	msg := fmt.Sprintf("dismissed a system alert %q with %q", alert.Title, alert.Dismissed)
	logger.Warn("%s", msg)
	fmt.Printf("    ⚠ %s\n", msg)
	record := report.SystemAlert{Title: alert.Title, Button: alert.Dismissed, Time: time.Now(), Step: step}
	fr.flowWriter.AddSystemAlert(record, cmdIndex)
	return alert, &record
}

// afterFailedLookup runs when a step has run. When it failed to find its
// element, it looks for a system alert once, and runs the step again with run
// when it dismissed one. It returns the step's result, a note for its error
// when an alert that was left alone covers the app, and the report's record
// of a dismissal.
func (fr *FlowRunner) afterFailedLookup(step flow.Step, desc string, cmdIndex int, result *core.CommandResult, run func() *core.CommandResult) (*core.CommandResult, string, *report.SystemAlert) {
	if !failedLookup(step, result) {
		return result, "", nil
	}
	if fr.ctx != nil && fr.ctx.Err() != nil {
		return result, "", nil // the run was stopped, or a runFlow ran out of time
	}
	alert, dismissal := fr.checkSystemAlert(desc, cmdIndex)
	if dismissal == nil {
		return result, systemAlertNote(alert), nil
	}
	logger.Info("running %s again, now the system alert is gone", desc)
	return run(), "", dismissal
}

// failedLookup reports whether a step failed because its element was not
// found or not visible, which is how a system alert over the app shows: the
// taps that should have moved the app on land on the alert instead.
func failedLookup(step flow.Step, result *core.CommandResult) bool {
	if result == nil || result.Success {
		return false
	}
	switch s := step.(type) {
	case *flow.RepeatStep, *flow.RetryStep, *flow.RunFlowStep:
		return false // each of their steps is checked as it fails
	case *flow.AssertNotVisibleStep:
		return false // an alert does not keep the app's elements on screen
	case *flow.WaitUntilStep:
		if s.Visible == nil {
			return false
		}
	}
	text := result.Message
	if result.Error != nil {
		text += " " + result.Error.Error()
	}
	text = strings.ToLower(text)
	return strings.Contains(text, "not found") || strings.Contains(text, "not visible")
}

// systemAlertNote is what a failed step's error adds about a system alert
// that covers the app and was left alone, or "" when there is none.
func systemAlertNote(alert *core.SystemAlert) string {
	if alert == nil || alert.Dismissed != "" {
		return ""
	}
	note := " (a system alert is covering the app: " + strconv.Quote(alert.Title)
	if len(alert.Buttons) > 0 {
		quoted := make([]string, len(alert.Buttons))
		for i, label := range alert.Buttons {
			quoted[i] = strconv.Quote(label)
		}
		note += ", buttons: " + strings.Join(quoted, ", ")
	}
	return note + ")"
}

// withSystemAlertNote adds note to a nested step's error, which is what its
// report entry, the console and an enclosing runFlow show.
func withSystemAlertNote(result *core.CommandResult, note string) *core.CommandResult {
	if note == "" {
		return result
	}
	noted := *result
	if result.Error != nil {
		noted.Error = fmt.Errorf("%w%s", result.Error, note)
	} else {
		noted.Error = fmt.Errorf("%s%s", result.Message, note)
	}
	return &noted
}
