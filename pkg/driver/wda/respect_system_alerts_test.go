package wda

import (
	"reflect"
	"testing"
)

func TestCreateSessionRespectsSystemAlertsWithTheSwitch(t *testing.T) {
	t.Setenv("MAESTRO_WDA_RESPECT_SYSTEM_ALERTS", "1")
	client, body := recordingServer(t)
	if err := client.CreateSession("com.example.app", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	settings, _ := body("/appium/settings")["settings"].(map[string]interface{})
	if settings["respectSystemAlerts"] != true {
		t.Errorf("settings = %v, want respectSystemAlerts=true", settings)
	}
	if settings["snapshotMaxDepth"] == nil {
		t.Error("snapshotMaxDepth is no longer sent with it")
	}
}

func TestCreateSessionLeavesSystemAlertsToWDAWithoutTheSwitch(t *testing.T) {
	client, body := recordingServer(t)
	if err := client.CreateSession("com.example.app", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	settings, _ := body("/appium/settings")["settings"].(map[string]interface{})
	if _, ok := settings["respectSystemAlerts"]; ok {
		t.Errorf("settings = %v: respectSystemAlerts sent without MAESTRO_WDA_RESPECT_SYSTEM_ALERTS", settings)
	}
}

// The system-alert check turns respectSystemAlerts on for its look. With this switch the run
// wants it on all along, so the check must leave it on, not turn it off.
func TestAppTargetingKeepsRespectingSystemAlertsWithTheSwitch(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	t.Setenv("MAESTRO_WDA_RESPECT_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, "")
	if _, err := f.driver.DismissSystemAlert(); err != nil {
		t.Fatalf("DismissSystemAlert: %v", err)
	}
	_, settings := f.seen()
	want := map[string]interface{}{"defaultActiveApplication": "auto", "respectSystemAlerts": true}
	if !reflect.DeepEqual(settings[len(settings)-1], want) {
		t.Errorf("last settings = %v, want %v", settings[len(settings)-1], want)
	}
}
