package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kthom1/switchyard/config"
)

func (a *Installation) Init(runner string) error {
	if err := a.Settings.Validate(); err != nil {
		return err
	}
	if err := prerequisites(); err != nil {
		return err
	}
	if !exists(filepath.Join(a.Root, "config.json")) {
		if err := checkPort("board", a.Settings.Port); err != nil {
			return err
		}
		if err := checkPort("runner", a.Settings.RunnerPort); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(a.Root, 0700); err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(a.Root); err == nil {
		a.Root = resolved
	}
	if err := a.copyFiles(a.Assets, "."); err != nil {
		return err
	}
	if err := a.installRunner(runner); err != nil {
		return err
	}
	if err := a.doctor(); err != nil {
		return err
	}
	if !exists(filepath.Join(a.Root, ".env.plane")) {
		volumes, err := a.command("docker", "volume", "ls", "--format", "{{.Name}}").Output()
		if err != nil {
			return fmt.Errorf("check existing Plane data volumes: %w", err)
		}
		retained := config.Name(a.Root) + "_pgdata"
		for _, volume := range strings.Fields(string(volumes)) {
			if volume == retained {
				return fmt.Errorf(".env.plane is missing but Plane data volume %s exists; restore the original configuration or use backup/restore with a new SWITCHYARD_HOME (see docs/backup.md)", retained)
			}
		}
	}
	data, _ := json.MarshalIndent(a.Settings, "", "  ")
	if err := writeOnce(filepath.Join(a.Root, "config.json"), append(data, '\n'), 0600); err != nil {
		return err
	}
	if !exists(filepath.Join(a.Root, ".env.plane")) {
		if err := a.configurePlane(); err != nil {
			return err
		}
	}
	if err := a.startBoard(); err != nil {
		return err
	}
	envExists := exists(filepath.Join(a.Root, ".env"))
	workflowExists := exists(filepath.Join(a.Root, "WORKFLOW.md"))
	if envExists != workflowExists {
		return errors.New("incomplete runner configuration: preserve .env and WORKFLOW.md and repair the missing file before retrying")
	}
	if !envExists {
		if _, err := os.Lstat(filepath.Join(a.Root, "plane.json")); errors.Is(err, os.ErrNotExist) {
			initialized, err := a.boardInitialized()
			if err != nil {
				return err
			}
			if initialized {
				return nil
			}
		} else if err != nil {
			return err
		}
		_, err := a.bootstrapPlane()
		return err
	}
	return nil
}
