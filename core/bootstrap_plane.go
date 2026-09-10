package core

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type localPlane struct {
	Workspace    string `json:"workspace"`
	WorkspaceID  string `json:"workspace_id"`
	ProjectID    string `json:"project_id"`
	Identifier   string `json:"identifier"`
	OwnerID      string `json:"owner_id"`
	AutomationID string `json:"automation_id"`
	OwnerEmail   string `json:"owner_email"`
	Password     string `json:"owner_password"`
	APIKey       string `json:"api_key"`
}

func (a Installation) bootstrapPlane() (localPlane, error) {
	path := filepath.Join(a.Root, "plane.json")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		settings := localPlane{Workspace: "switchyard", Identifier: "APP", OwnerEmail: "owner@switchyard.local"}
		for _, id := range []*string{&settings.WorkspaceID, &settings.ProjectID, &settings.OwnerID, &settings.AutomationID} {
			var value [16]byte
			if _, err := rand.Read(value[:]); err != nil {
				return localPlane{}, err
			}
			value[6] = value[6]&0x0f | 0x40
			value[8] = value[8]&0x3f | 0x80
			*id = fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:])
		}
		for _, secret := range []*string{&settings.Password, &settings.APIKey} {
			var value [32]byte
			if _, err := rand.Read(value[:]); err != nil {
				return localPlane{}, err
			}
			*secret = hex.EncodeToString(value[:])
		}
		settings.APIKey = "plane_api_" + settings.APIKey
		data, _ := json.MarshalIndent(settings, "", "  ")
		// Persist the generated identity before provisioning so interrupted setup can resume.
		if err := writeOnce(path, append(data, '\n'), 0600); err != nil {
			return localPlane{}, err
		}
	} else if err != nil {
		return localPlane{}, err
	}
	settings, err := a.readLocalPlane()
	if err != nil {
		return localPlane{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return localPlane{}, err
	}
	if err := a.runPlaneSetup(data); err != nil {
		return localPlane{}, err
	}
	return settings, nil
}

func (a Installation) readLocalPlane() (localPlane, error) {
	path := filepath.Join(a.Root, "plane.json")
	info, err := os.Lstat(path)
	if err != nil {
		return localPlane{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return localPlane{}, errors.New("plane.json must be a regular private file (chmod 600); existing credentials are preserved")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return localPlane{}, err
	}
	var settings localPlane
	if err := json.Unmarshal(data, &settings); err != nil {
		return localPlane{}, errors.New("invalid plane.json; restore the original file before retrying")
	}
	return settings, nil
}

func (a Installation) runPlaneSetup(data []byte) error {
	code, err := os.ReadFile(filepath.Join(a.Root, "scripts/bootstrap-plane.py"))
	if err != nil {
		return err
	}
	command := a.command(filepath.Join(a.Root, "scripts/plane"), "exec", "-T", "api", "python", "manage.py", "shell", "-c", string(code))
	command.Stdin = bytes.NewReader(data)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("local Plane setup failed; fix the reported cause and rerun the command (plane.json is preserved): %w", err)
	}
	return nil
}

func (a Installation) provisionProject(plane localPlane, options *ProjectOptions) error {
	existing := options.Project != ""
	if !existing {
		var err error
		options.Project, err = projectUUID(plane.WorkspaceID, options.Repo)
		if err != nil {
			return err
		}
	}
	if options.Identifier == "" {
		options.Identifier = projectIdentifier(options.Repo)
	}
	request := struct {
		Board   localPlane `json:"board"`
		Project struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
			Name       string `json:"name"`
			Existing   bool   `json:"existing"`
		} `json:"project"`
	}{Board: plane}
	request.Project.ID, request.Project.Identifier = options.Project, options.Identifier
	request.Project.Name = repositoryName(options.Repo)
	request.Project.Existing = existing
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return a.runPlaneSetup(data)
}
