package core

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kthom1/switchyard/config"
)

func runnerConfigurationTest(t *testing.T, handler http.HandlerFunc) (Installation, ProjectOptions) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if err := os.Mkdir(filepath.Join(root, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), data, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("scripts/task-workspace", []byte("#!/bin/sh\n/bin/mkdir -p \"$SYMPHONY_WORKSPACE_ROOT\"\n"), 0700)
	template, err := os.ReadFile("../WORKFLOW.example.md")
	if err != nil {
		t.Fatal(err)
	}
	write("WORKFLOW.example.md", template, 0600)
	write("key", []byte("new-synthetic-key\n"), 0600)
	input, err := os.Open(filepath.Join(root, "key"))
	if err != nil {
		t.Fatal(err)
	}
	previousStdin := os.Stdin
	os.Stdin = input
	t.Cleanup(func() { os.Stdin = previousStdin; input.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	a := Installation{Root: root, Settings: config.Settings{Port: server.Listener.Addr().(*net.TCPAddr).Port, RunnerPort: 8091, WebURL: "http://localhost:8090"}}
	options := ProjectOptions{Repo: "https://github.com/example/new", Workspace: "example", Project: "12345678-1234-1234-1234-123456789abc", Identifier: "NEW", APIKeyStdin: true}
	return a, options
}

func TestConfigureRunnerChecksProjectBeforeSaving(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"valid", `{"id":"12345678-1234-1234-1234-123456789abc","identifier":"NEW"}`, "", 200},
		{"invalid key", "new-synthetic-key", "HTTP 401", 401},
		{"no access", "new-synthetic-key", "HTTP 403", 403},
		{"missing project", "new-synthetic-key", "HTTP 404", 404},
		{"unavailable", "new-synthetic-key", "HTTP 503", 503},
		{"redirect", "new-synthetic-key", "HTTP 302", 302},
		{"malformed", "new-synthetic-key", "invalid project", 200},
		{"wrong project", `{"id":"other","identifier":"NEW"}`, "invalid project", 200},
		{"wrong identifier", `{"id":"12345678-1234-1234-1234-123456789abc","identifier":"OTHER"}`, "identifier does not match", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			a, options := runnerConfigurationTest(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/workspaces/example/projects/12345678-1234-1234-1234-123456789abc/" || r.Header.Get("X-API-Key") != "new-synthetic-key" {
					t.Error("project check must make an authenticated GET for the configured project")
				}
				w.Header().Set("Location", "http://"+r.Host+"/redirected")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			err := a.configureRunner(options)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "new-synthetic-key") {
				t.Fatal("project check exposed the API key or response body")
			}
			if requests.Load() != 1 {
				t.Fatal("project check must not retry or follow redirects")
			}
			for _, name := range []string{".env", "WORKFLOW.md"} {
				_, statErr := os.Stat(filepath.Join(a.Root, name))
				if tc.want == "" && statErr != nil || tc.want != "" && !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("unexpected saved %s after project check: %v", name, statErr)
				}
			}
		})
	}
}

func TestConfigureRunnerRefusesConfigurationCreatedWhileReadingKey(t *testing.T) {
	var root string
	previous := "PLANE_API_KEY=previous-synthetic-key\nSOURCE_REPO_URL=https://github.com/example/previous\n"
	a, options := runnerConfigurationTest(t, func(w http.ResponseWriter, r *http.Request) {
		// Another connection publishes its environment after the caller checked both files.
		if err := os.WriteFile(filepath.Join(root, ".env"), []byte(previous), 0600); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"id":"12345678-1234-1234-1234-123456789abc","identifier":"NEW"}`)
	})
	root = a.Root
	if err := a.configureRunner(options); !errors.Is(err, os.ErrExist) {
		t.Fatalf("configuration created by another connection must prevent publishing a different workflow: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, ".env")); err != nil || string(data) != previous {
		t.Fatalf("existing environment changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "WORKFLOW.md")); !os.IsNotExist(err) {
		t.Fatalf("a workflow was published beside another connection's environment: %v", err)
	}
}
