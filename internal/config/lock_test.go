package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveModrinthSnakeCaseResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version/version-id" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"version-id",
			"project_id":"project-id",
			"game_versions":["1.21.1"],
			"loaders":["fabric"],
			"files":[{"primary":true,"hashes":{"sha512":"abc123"}}]
		}`))
	}))
	defer server.Close()

	resolver := Resolver{Client: server.Client(), ModrinthBase: server.URL}
	got, err := resolver.resolveModrinth(context.Background(), Pack{
		Provider: "modrinth", ProjectID: "project-id", VersionID: "version-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "project-id" || got.Minecraft != "1.21.1" || got.Loader != "fabric" || got.ArtifactHash != "sha512:abc123" {
		t.Fatalf("unexpected lock data: %#v", got)
	}
}
