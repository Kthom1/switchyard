package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectConnectionsPreserveWorkflowAndRejectRemapping(t *testing.T) {
	const secondID = "22345678-1234-1234-1234-123456789abc"
	const thirdID = "32345678-1234-1234-1234-123456789abc"
	requests := 0
	a, first := runnerConfigurationTest(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-API-Key") != "new-synthetic-key" {
			t.Error("project access must use the installed credential")
		}
		id, identifier := "12345678-1234-1234-1234-123456789abc", "NEW"
		if strings.Contains(r.URL.Path, secondID) {
			id, identifier = secondID, "SECOND"
		}
		if strings.Contains(r.URL.Path, thirdID) {
			id, identifier = thirdID, "THIRD"
		}
		fmt.Fprintf(w, `{"id":%q,"identifier":%q}`, id, identifier)
	})
	t.Setenv("PATH", "/usr/bin:/bin")
	if err := a.configureRunner(first); err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(a.Root, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	env := string(read(".env"))
	path := filepath.Join(a.Root, "WORKFLOW.md")
	custom := strings.Replace(string(read("WORKFLOW.md")), "max_concurrent_agents: 1", "max_concurrent_agents: 3 # my setting", 1) + "\nA custom prompt with --- inside it.\n"
	if err := os.WriteFile(path, []byte(custom), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := readWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	second := ProjectOptions{Repo: "https://github.com/example/second", Project: secondID, Identifier: "SECOND"}
	if err := a.connectProject(second); err != nil {
		t.Fatal(err)
	}
	after, err := readWorkflow(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.body != after.body || field(field(after.document.Content[0], "agent"), "max_concurrent_agents").Value != "3" || !strings.Contains(string(read("WORKFLOW.md")), "# my setting") {
		t.Fatal("connecting a project changed custom workflow settings or prompt")
	}
	if string(read(".env")) != env {
		t.Fatal("adding projects changed shared credentials")
	}
	saved := string(read("WORKFLOW.md"))
	for _, repeat := range []ProjectOptions{first, second, {Repo: second.Repo}} {
		if err := a.connectProject(repeat); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 2 || string(read("WORKFLOW.md")) != saved {
		t.Fatal("repeated connection was not an unchanged no-op")
	}
	for _, conflict := range []ProjectOptions{
		{Repo: "https://github.com/example/wrong", Project: secondID, Identifier: "SECOND"},
		{Repo: second.Repo, Project: secondID, Identifier: "OTHER"},
		{Repo: second.Repo, Identifier: "OTHER"},
		{Repo: "https://github.com/example/third", Project: thirdID, Identifier: "SECOND"},
	} {
		if err := a.connectProject(conflict); err == nil {
			t.Fatal("conflicting mapping was accepted")
		}
	}
	if requests != 2 || string(read("WORKFLOW.md")) != saved {
		t.Fatal("conflicting connection performed external writes or changed mappings")
	}
	// Multiple explicitly selected projects may intentionally use one repository.
	if err := a.connectProject(ProjectOptions{Repo: second.Repo, Project: thirdID, Identifier: "THIRD"}); err != nil {
		t.Fatal(err)
	}
	values, err := a.Projects()
	if err != nil || len(values) != 3 || values[0].Repo != first.Repo || values[1].ID != secondID || values[2].ID != thirdID {
		t.Fatalf("incorrect connections: %v %v", values, err)
	}
	if err := after.save(path); err == nil {
		t.Fatal("stale workflow snapshot overwrote a later connection")
	}
}

func TestProjectProvisioningRetryKeepsIdentityAndCredentials(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"scripts/bootstrap-plane.py": "# script is passed separately from credentials",
		"scripts/plane":              "#!/bin/sh\n/bin/cat > received.json\nexit 1\n",
		"plane.json":                 "original board credentials stay unchanged\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0700); err != nil {
			t.Fatal(err)
		}
	}
	a := Installation{Root: root}
	board := localPlane{WorkspaceID: "12345678-1234-1234-1234-123456789abc"}
	var saved []byte
	for attempt := 0; attempt < 2; attempt++ {
		options := ProjectOptions{Repo: "https://github.com/example/second"}
		if err := a.provisionProject(board, &options); err == nil {
			t.Fatal("failed provisioning should report failure")
		}
		data, err := os.ReadFile(filepath.Join(root, "received.json"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt > 0 && string(data) != string(saved) {
			t.Fatal("retry changed its project provisioning identity")
		}
		saved = data
	}
	data, err := os.ReadFile(filepath.Join(root, "plane.json"))
	if err != nil || string(data) != "original board credentials stay unchanged\n" {
		t.Fatal("project provisioning changed board credentials")
	}
}

func TestWorkflowRejectsEquivalentProjectIdentities(t *testing.T) {
	const id = "a2345678-1234-1234-1234-123456789abc"
	for _, second := range []string{id, strings.ToUpper(id), "{" + id + "}"} {
		t.Run(second, func(t *testing.T) {
			root := t.TempDir()
			data := fmt.Sprintf("---\ntracker:\n  kind: plane\n  provider:\n    projects:\n      - {project_id: %q, project_identifier: ONE, repo: https://github.com/example/one}\n      - {project_id: %q, project_identifier: TWO, repo: https://github.com/example/two}\n---\nPrompt.\n", id, second)
			path := filepath.Join(root, "WORKFLOW.md")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			w, err := readWorkflow(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (Installation{Root: root}).workflowProjects(w); err == nil {
				t.Fatal("equivalent project identities were accepted as different routes")
			}
		})
	}
}

func TestManualProjectConnectionPreservesUnrelatedLocalCredentials(t *testing.T) {
	const secondID = "22345678-1234-1234-1234-123456789abc"
	requests := 0
	a, options := runnerConfigurationTest(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-API-Key") != "new-synthetic-key" {
			t.Error("manual project must use the installed runner credential")
		}
		id, identifier := "12345678-1234-1234-1234-123456789abc", "NEW"
		if strings.Contains(r.URL.Path, secondID) {
			id, identifier = secondID, "SECOND"
		}
		fmt.Fprintf(w, `{"id":%q,"identifier":%q}`, id, identifier)
	})
	t.Setenv("PATH", "/usr/bin:/bin")
	if err := a.configureRunner(options); err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(localPlane{Workspace: "example", APIKey: "other-local-board-token"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.Root, "plane.json")
	if err := os.WriteFile(path, metadata, 0600); err != nil {
		t.Fatal(err)
	}
	// No provisioning script exists: this connection must use only authenticated reads.
	second := ProjectOptions{Repo: "https://github.com/example/second", Project: secondID, Identifier: "SECOND"}
	if err := a.connectProject(second); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatal("manual connection did not verify project access")
	}
	if err := a.connectProject(ProjectOptions{Repo: "https://github.com/example/third"}); err == nil {
		t.Fatal("automatic provisioning accepted unrelated local credentials")
	}
	if requests != 2 {
		t.Fatal("refused automatic provisioning called the API")
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != string(metadata) {
		t.Fatal("manual connection changed local board credentials")
	}
}
