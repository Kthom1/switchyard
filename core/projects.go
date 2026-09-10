package core

import (
	"crypto/sha1" // UUIDv5 names project identities; this is not a security hash.
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

func (a Installation) Projects() ([]Project, error) {
	if !a.RunnerConfigured() {
		return nil, nil
	}
	w, err := readWorkflow(filepath.Join(a.Root, "WORKFLOW.md"))
	if err != nil {
		return nil, err
	}
	return a.workflowProjects(w)
}

func (a Installation) AddProject(options ProjectOptions) error {
	if options.Repo == "" {
		return errors.New("provide --repo URL")
	}
	if err := options.validate(); err != nil {
		return err
	}
	if err := a.startBoard(); err != nil {
		return err
	}
	return a.connectProject(options)
}

func (a Installation) connectProject(options ProjectOptions) error {
	lock, err := os.OpenFile(filepath.Join(a.Root, ".projects.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if exists(filepath.Join(a.Root, ".env")) != exists(filepath.Join(a.Root, "WORKFLOW.md")) {
		return errors.New("incomplete runner configuration: repair .env and WORKFLOW.md before connecting a project")
	}
	if !a.RunnerConfigured() {
		if options.Workspace == "" {
			plane, err := a.bootstrapPlane()
			if err != nil {
				return err
			}
			options.Workspace, options.apiKey = plane.Workspace, plane.APIKey
			if options.Project == "" && options.Identifier == "" {
				options.Project, options.Identifier = plane.ProjectID, plane.Identifier
			} else if err := a.provisionProject(plane, &options); err != nil {
				return err
			}
		}
		return a.configureRunner(options)
	}
	w, err := readWorkflow(filepath.Join(a.Root, "WORKFLOW.md"))
	if err != nil {
		return err
	}
	projects, err := a.workflowProjects(w)
	if err != nil {
		return err
	}
	workspace, endpoint, keyNode := field(w.provider, "workspace"), field(w.provider, "endpoint"), field(w.provider, "api_key")
	if workspace == nil || endpoint == nil || keyNode == nil {
		return errors.New("workflow is missing shared Plane connection settings")
	}
	if options.Workspace != "" && options.Workspace != workspace.Value {
		return errors.New("this installation uses another Plane workspace; existing configuration is preserved")
	}
	options.Workspace = workspace.Value
	if endpoint.Value != fmt.Sprintf("http://127.0.0.1:%d", a.Settings.Port) {
		return errors.New("workflow endpoint differs from this local board; existing configuration is preserved")
	}
	// An explicit project allows two projects to use the same repository.
	for _, project := range projects {
		if options.Project != "" && options.Project == project.ID {
			if options.Repo != project.Repo || options.Identifier != project.Identifier {
				return errors.New("project is already connected with a different repository or identifier; existing mapping is preserved")
			}
			return nil
		}
	}
	if options.Project == "" {
		for _, project := range projects {
			if project.Repo == options.Repo && (options.Identifier == "" || options.Identifier == project.Identifier) {
				return nil
			}
		}
		for _, project := range projects {
			if project.Repo == options.Repo {
				return errors.New("repository is already connected with another identifier; use its existing identifier or an explicit --project-id")
			}
		}
	}
	if options.Identifier == "" {
		options.Identifier = projectIdentifier(options.Repo)
	}
	for _, project := range projects {
		if project.Identifier == options.Identifier {
			return errors.New("project identifier is already connected; choose another --identifier")
		}
	}
	key := keyNode.Value
	if strings.HasPrefix(key, "$") {
		name := strings.TrimPrefix(key, "$")
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name) {
			return errors.New("unsupported workflow API key environment reference")
		}
		key, err = a.runnerVariable(name)
		if err != nil {
			return err
		}
	}
	if key == "" {
		return errors.New("saved Plane API key is empty")
	}
	if exists(filepath.Join(a.Root, "plane.json")) {
		plane, err := a.readLocalPlane()
		if err == nil && plane.Workspace == options.Workspace && plane.APIKey == key {
			if err := a.provisionProject(plane, &options); err != nil {
				return err
			}
		} else if options.Project == "" {
			if err != nil {
				return err
			}
			return errors.New("local board credentials do not match the runner; use --project-id and --identifier for a manual connection")
		}
	} else if options.Project == "" {
		return errors.New("automatic project creation requires this installation's plane.json; use --project-id and --identifier to connect an existing project")
	}
	if err := a.verifyProject(options, key); err != nil {
		return err
	}
	projects = append(projects, Project{ID: options.Project, Identifier: options.Identifier, Repo: options.Repo})
	if err := setField(w.provider, "projects", projects); err != nil {
		return err
	}
	removeField(w.provider, "project_id")
	removeField(w.provider, "project_identifier")
	return w.save(filepath.Join(a.Root, "WORKFLOW.md"))
}

func projectIdentifier(repo string) string {
	name := repositoryName(repo)
	name = regexp.MustCompile(`[^A-Z0-9_]`).ReplaceAllString(strings.ToUpper(name), "")
	if name == "" || name[0] < 'A' || name[0] > 'Z' {
		name = "APP" + name
	}
	if len(name) > 12 {
		name = name[:12]
	}
	return name
}

func repositoryName(repo string) string {
	name := strings.TrimSuffix(strings.TrimRight(repo, "/"), ".git")
	return name[strings.LastIndexAny(name, "/:")+1:]
}

func projectUUID(workspace, repo string) (string, error) {
	namespace, err := hex.DecodeString(strings.ReplaceAll(workspace, "-", ""))
	if err != nil || len(namespace) != 16 {
		return "", errors.New("invalid local Plane workspace UUID")
	}
	id := sha1.Sum(append(namespace, []byte(repo)...))
	id[6] = id[6]&0x0f | 0x50
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:16]), nil
}
