package flymachine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hostpack/hostpack/internal/config"
)

func TestEnsureLeavesMatchingMachineAlone(t *testing.T) {
	posts := 0
	server := machineServer(t, 4096, 2, &posts, nil)
	defer server.Close()
	client, err := New(server.URL, "app", "machine", "secret-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := client.Ensure(context.Background(), config.Pack{MachineMemoryMB: 4096, MachineCPUs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if changed || posts != 0 {
		t.Fatalf("matching Machine was updated: changed=%v posts=%d", changed, posts)
	}
}

func TestEnsureUpdatesResourcesAndPreservesConfig(t *testing.T) {
	posts := 0
	var posted map[string]any
	server := machineServer(t, 2048, 1, &posts, &posted)
	defer server.Close()
	client, err := New(server.URL, "app", "machine", "secret-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := client.Ensure(context.Background(), config.Pack{MachineMemoryMB: 4096, MachineCPUs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !changed || posts != 1 {
		t.Fatalf("resources were not updated: changed=%v posts=%d", changed, posts)
	}
	payloadConfig := posted["config"].(map[string]any)
	guest := payloadConfig["guest"].(map[string]any)
	if guest["memory_mb"] != float64(4096) || guest["cpus"] != float64(2) || guest["cpu_kind"] != "shared" {
		t.Fatalf("unexpected guest update: %#v", guest)
	}
	if payloadConfig["image"] != "registry.example/hostpack@sha256:abc" {
		t.Fatalf("Machine config was not preserved: %#v", payloadConfig)
	}
}

func machineServer(t *testing.T, memory, cpus int, posts *int, posted *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/app/machines/machine" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("missing authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{
				"image":  "registry.example/hostpack@sha256:abc",
				"guest":  map[string]any{"cpu_kind": "shared", "cpus": cpus, "memory_mb": memory},
				"mounts": []any{map[string]any{"volume": "vol_1", "path": "/state"}},
			}})
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %q", r.Method)
		}
		*posts++
		if posted != nil {
			if err := json.NewDecoder(r.Body).Decode(posted); err != nil {
				t.Errorf("decode update: %v", err)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
}
