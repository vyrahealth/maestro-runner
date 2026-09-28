package wda

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// recordingServer answers every WDA call and keeps each POST body by path.
func recordingServer(t *testing.T) (*Client, func(string) map[string]interface{}) {
	t.Helper()
	var mu sync.Mutex
	bodies := map[string]map[string]interface{}{}
	server := mockWDAServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]interface{}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			bodies[r.URL.Path] = body
			mu.Unlock()
		}
		jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"sessionId": "s1"}})
	})
	t.Cleanup(server.Close)
	client := &Client{baseURL: server.URL, httpClient: http.DefaultClient}
	return client, func(suffix string) map[string]interface{} {
		mu.Lock()
		defer mu.Unlock()
		for path, body := range bodies {
			if strings.HasSuffix(path, suffix) {
				return body
			}
		}
		return nil
	}
}

func sessionCaps(body map[string]interface{}) map[string]interface{} {
	caps, _ := body["capabilities"].(map[string]interface{})
	alwaysMatch, _ := caps["alwaysMatch"].(map[string]interface{})
	return alwaysMatch
}

func TestCreateSessionSendsTheLaunchEnv(t *testing.T) {
	t.Setenv("MAESTRO_WDA_LAUNCH_ENV", `{"LIB_PATH":"/a:/b"}`)
	client, body := recordingServer(t)
	if err := client.CreateSession("com.example.app", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	env, _ := sessionCaps(body("/session"))["environment"].(map[string]interface{})
	if env["LIB_PATH"] != "/a:/b" {
		t.Errorf("session environment = %v, want LIB_PATH=/a:/b", env)
	}
}

func TestCreateSessionWithoutTheSwitchSendsNoEnvironment(t *testing.T) {
	client, body := recordingServer(t)
	if err := client.CreateSession("com.example.app", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, ok := sessionCaps(body("/session"))["environment"]; ok {
		t.Error("environment sent without MAESTRO_WDA_LAUNCH_ENV")
	}
}

func TestLaunchMergesTheLaunchEnvUnderTheFlows(t *testing.T) {
	t.Setenv("MAESTRO_WDA_LAUNCH_ENV", `{"LIB_PATH":"/a","MODE":"switch"}`)
	client, body := recordingServer(t)
	client.sessionID = "s1"
	if err := client.LaunchAppWithArgs("com.example.app", []string{"-k", "v"}, map[string]string{"MODE": "flow"}); err != nil {
		t.Fatalf("LaunchAppWithArgs: %v", err)
	}
	env, _ := body("/wda/apps/launch")["environment"].(map[string]interface{})
	if env["LIB_PATH"] != "/a" || env["MODE"] != "flow" || len(env) != 2 {
		t.Errorf("launch environment = %v, want LIB_PATH=/a and the flow's MODE=flow", env)
	}
}

func TestLaunchWithOnlyTheSwitchStillSendsIt(t *testing.T) {
	t.Setenv("MAESTRO_WDA_LAUNCH_ENV", `{"LIB_PATH":"/a"}`)
	client, body := recordingServer(t)
	client.sessionID = "s1"
	if err := client.LaunchApp("com.example.app"); err != nil {
		t.Fatalf("LaunchApp: %v", err)
	}
	env, _ := body("/wda/apps/launch")["environment"].(map[string]interface{})
	if env["LIB_PATH"] != "/a" {
		t.Errorf("launch environment = %v, want LIB_PATH=/a", env)
	}
}

func TestALaunchEnvThatIsNotAnObjectIsIgnored(t *testing.T) {
	for _, v := range []string{`LIB_PATH=/a`, `["a"]`, `{"N":1}`} {
		t.Setenv("MAESTRO_WDA_LAUNCH_ENV", v)
		if env := wdaLaunchEnv(); env != nil {
			t.Errorf("wdaLaunchEnv() for %q = %v, want nil", v, env)
		}
	}
}
