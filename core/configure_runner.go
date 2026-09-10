package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a Installation) configureRunner(options ProjectOptions) error {
	taskRoot := filepath.Join(a.Root, "workspaces")
	check := a.command(filepath.Join(a.Root, "scripts/task-workspace"), "root")
	check.Env = append(check.Env, "SYMPHONY_WORKSPACE_ROOT="+taskRoot)
	check.Stdout, check.Stderr = os.Stdout, os.Stderr
	if err := check.Run(); err != nil {
		return fmt.Errorf("check task workspace root: %w", err)
	}
	taskRoot, err := filepath.EvalSymlinks(taskRoot)
	if err != nil {
		return err
	}
	workflow, err := readWorkflow(filepath.Join(a.Root, "WORKFLOW.example.md"))
	if err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{"endpoint", fmt.Sprintf("http://127.0.0.1:%d", a.Settings.Port)},
		{"web_url", a.Settings.WebURL},
		{"workspace", options.Workspace},
	} {
		if err := setField(workflow.provider, pair[0], pair[1]); err != nil {
			return err
		}
	}
	removeField(workflow.provider, "project_id")
	removeField(workflow.provider, "project_identifier")
	if err := setField(workflow.provider, "projects", []Project{{ID: options.Project, Identifier: options.Identifier, Repo: options.Repo}}); err != nil {
		return err
	}
	if err := setField(field(workflow.document.Content[0], "server"), "port", a.Settings.RunnerPort); err != nil {
		return err
	}
	key := options.apiKey
	if key == "" {
		key, err = readAPIKey(options.APIKeyStdin)
		if err != nil {
			return err
		}
	}
	if err := a.verifyProject(options, key); err != nil {
		return err
	}
	var env strings.Builder
	for _, pair := range [][2]string{{"PLANE_API_KEY", key}, {"SYMPHONY_WORKSPACE_ROOT", taskRoot}} {
		// These values are sourced by Bash; single quotes preserve them literally.
		fmt.Fprintf(&env, "%s='%s'\n", pair[0], strings.ReplaceAll(pair[1], "'", "'\"'\"'"))
	}
	data, err := workflow.bytes()
	if err != nil {
		return err
	}
	if err := writeNew(filepath.Join(a.Root, ".env"), []byte(env.String()), 0600); err != nil {
		return err
	}
	return writeNew(filepath.Join(a.Root, "WORKFLOW.md"), data, 0600)
}

func (a Installation) verifyProject(options ProjectOptions, key string) error {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/api/v1/workspaces/%s/projects/%s/", a.Settings.Port, options.Workspace, options.Project)
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("could not prepare the Plane project access check")
	}
	request.Header.Set("X-API-Key", key)
	client := http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("could not verify Plane project access; check switchyard logs plane and rerun project add")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Plane project access check failed (HTTP %d); check the workspace, project ID and Plane API key, then rerun project add", response.StatusCode)
	}
	var project struct{ ID, Identifier string }
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&project); err != nil || project.ID != options.Project {
		return errors.New("Plane returned an invalid project; check the workspace and project ID, then rerun project add")
	}
	if project.Identifier != options.Identifier {
		return errors.New("Plane project identifier does not match --identifier; check the project's short identifier and rerun project add")
	}
	return nil
}
