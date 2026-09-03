package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hostpack/hostpack/internal/config"
	hpruntime "github.com/hostpack/hostpack/internal/runtime"
)

type fakeState struct {
	state   hpruntime.State
	touches int
}

func (f *fakeState) State() hpruntime.State { return f.state }
func (f *fakeState) Touch()                 { f.touches++ }

func TestDashboardRequiresAuthentication(t *testing.T) {
	logs := testLogBuffer(t)
	server := New(testConfig(), &fakeState{}, logs, "hostpack", "correct horse battery staple").Handler()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if recorder.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing Basic authentication challenge")
	}
}

func TestDashboardStatusAndLogs(t *testing.T) {
	logs := testLogBuffer(t)
	_, _ = logs.Write([]byte(`{"time":"2026-09-03T14:00:00Z","level":"INFO","msg":"ready"}` + "\n"))
	state := &fakeState{state: hpruntime.State{Phase: hpruntime.Ready, ActiveID: "alpha", Generation: 3}}
	server := New(testConfig(), state, logs, "hostpack", "correct horse battery staple").Handler()

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	statusRequest.SetBasicAuth("hostpack", "correct horse battery staple")
	statusRecorder := httptest.NewRecorder()
	server.ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	var response statusResponse
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.State.Phase != hpruntime.Ready || len(response.Packs) != 1 || response.Packs[0].MachineMemoryMB != 3072 {
		t.Fatalf("unexpected response: %+v", response)
	}
	if state.touches != 1 {
		t.Fatalf("authenticated request touches = %d, want 1", state.touches)
	}

	logRequest := httptest.NewRequest(http.MethodGet, "/api/logs?limit=1", nil)
	logRequest.SetBasicAuth("hostpack", "correct horse battery staple")
	logRecorder := httptest.NewRecorder()
	server.ServeHTTP(logRecorder, logRequest)
	if logRecorder.Code != http.StatusOK || !json.Valid(logRecorder.Body.Bytes()) {
		t.Fatalf("invalid log response: %d %s", logRecorder.Code, logRecorder.Body.String())
	}
	if logRecorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing security headers")
	}
}

func TestHealthDoesNotRequireAuthenticationOrKeepMachineAwake(t *testing.T) {
	logs := testLogBuffer(t)
	state := &fakeState{}
	server := New(testConfig(), state, logs, "hostpack", "correct horse battery staple").Handler()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK || state.touches != 0 {
		t.Fatalf("health response = %d, touches = %d", recorder.Code, state.touches)
	}
}

func testConfig() *config.Config {
	return &config.Config{Domain: "mc.example.com", Packs: map[string]config.Pack{
		"alpha": {DisplayName: "Alpha", Provider: "modrinth", Java: 21, MemoryMB: 2048, MachineMemoryMB: 3072, MachineCPUs: 2},
	}}
}

func testLogBuffer(t *testing.T) *LogBuffer {
	t.Helper()
	logs, err := NewLogBuffer(filepath.Join(t.TempDir(), "dashboard.log"), 10, 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logs.Close() })
	return logs
}
