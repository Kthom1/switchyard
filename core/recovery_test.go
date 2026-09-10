package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kthom1/switchyard/config"
)

func TestRestoreRefusesExistingInstallationOrService(t *testing.T) {
	for _, existing := range []string{"directory", "service"} {
		t.Run(existing, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PATH", t.TempDir())
			write := func(path, contents string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			backup, root := t.TempDir(), t.TempDir()
			write(filepath.Join(backup, "config/config.json"), `{"port":8090,"runner_port":8091,"web_url":"http://localhost:8090"}`)
			a := Installation{Root: root, Assets: os.DirFS(t.TempDir())}
			occupiedPath := filepath.Join(root, "keep.txt")
			wantError, wantEntries := "empty SWITCHYARD_HOME", 1
			if existing == "service" {
				var err error
				occupiedPath, err = config.UnitPath(root)
				if err != nil {
					t.Fatal(err)
				}
				// A different spelling of the destination must find the same service.
				a.Root = filepath.Join(t.TempDir(), "destination")
				if err := os.Symlink(root, a.Root); err != nil {
					t.Fatal(err)
				}
				wantError, wantEntries = "service", 0
			}
			write(occupiedPath, "preserve this existing file\n")
			if err := a.Restore(backup, ""); err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("restore returned %v; want %q refusal", err, wantError)
			}
			if data, err := os.ReadFile(occupiedPath); err != nil || string(data) != "preserve this existing file\n" {
				t.Fatalf("restore changed existing data: %q, %v", data, err)
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != wantEntries {
				t.Fatalf("refused restore wrote destination files: %v, %v", entries, err)
			}
		})
	}
}

func TestCLIRecoveryKeepsPersonalCodexHomesOutsideItsScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	personal, legacy := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", personal)
	t.Setenv("SWITCHYARD_CODEX_HOME", legacy)
	write := func(path, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, home := range []string{personal, legacy} {
		write(filepath.Join(home, "auth.json"), "personal sentinel")
	}
	root, assets, backup := t.TempDir(), t.TempDir(), t.TempDir()
	write(filepath.Join(root, ".env"), "")
	write(filepath.Join(root, "WORKFLOW.md"), "workflow")
	write(filepath.Join(root, "scripts/plane"), "#!/bin/sh\nexit 0\n")
	scope := "#!/bin/sh\n[ \"$SWITCHYARD_CODEX_HOME\" = \"$PWD/work/codex\" ] || exit 41\nprintf '%s' \"$SWITCHYARD_CODEX_HOME\" > scope\n"
	write(filepath.Join(root, "scripts/backup"), scope)
	a := Installation{Root: root}
	if err := a.Backup("", "test"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "scope")); err != nil || string(data) != filepath.Join(root, "work/codex") {
		t.Fatalf("backup escaped installation-owned home: %q, %v", data, err)
	}
	write(filepath.Join(backup, "config/config.json"), `{"port":8090,"runner_port":8091,"web_url":"http://localhost:8090"}`)
	write(filepath.Join(assets, "scripts/task-workspace"), "#!/bin/sh\nexit 0\n")
	write(filepath.Join(assets, "scripts/restore"), scope+"exit 42\n")
	bundle := t.TempDir()
	write(filepath.Join(bundle, "bin/symphony"), "runner")
	write(filepath.Join(bundle, "licenses/NOTICE"), "notice")
	a = Installation{Root: t.TempDir(), Assets: os.DirFS(assets)}
	if err := a.Restore(backup, filepath.Join(bundle, "bin/symphony")); err == nil || !strings.Contains(err.Error(), "exit status 42") {
		t.Fatalf("restore did not reach the scoped script: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(a.Root, "scope")); err != nil || string(data) != filepath.Join(a.Root, "work/codex") {
		t.Fatalf("restore escaped installation-owned home: %q, %v", data, err)
	}
	for _, home := range []string{personal, legacy} {
		if data, err := os.ReadFile(filepath.Join(home, "auth.json")); err != nil || string(data) != "personal sentinel" {
			t.Fatalf("personal auth changed: %q, %v", data, err)
		}
	}
}
