package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexHomeUsesNativeDefaultAndResolvesCallerRelativeOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	t.Setenv("SWITCHYARD_CODEX_HOME", "")
	home, err := codexHome()
	if err != nil || home != filepath.Join(os.Getenv("HOME"), ".codex") {
		t.Fatalf("default home: %q, %v", home, err)
	}
	caller := t.TempDir()
	t.Chdir(caller)
	t.Setenv("CODEX_HOME", "custom-codex")
	t.Setenv("SWITCHYARD_CODEX_HOME", filepath.Join(t.TempDir(), "legacy"))
	a := Installation{Root: t.TempDir()}
	home, err = codexHome()
	want := filepath.Join(caller, "custom-codex")
	if err != nil || home != want {
		t.Fatalf("native override: %q, %v", home, err)
	}
	data, err := a.command("/bin/sh", "-c", `printf '%s' "$CODEX_HOME"`).Output()
	if err != nil || string(data) != want {
		t.Fatalf("command changed relative home after cwd change: %q, %v", data, err)
	}
}

func TestRunnerServicePreservesItsSelectedHomeAndLoginScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	t.Setenv("SWITCHYARD_CODEX_HOME", "")
	t.Setenv("PATH", "/usr/bin:/bin")
	a := Installation{Root: t.TempDir()}
	template := "WorkingDirectory=@WORKING_DIRECTORY@\nExecStart=@INSTALL_ROOT@/scripts/run\nEnvironment=PATH=%h/.local/share/mise/shims:%h/.local/bin:/usr/local/bin:/usr/bin:/bin\n"
	unit, home, err := a.runnerUnit(template, "")
	defaultHome := filepath.Join(os.Getenv("HOME"), ".codex")
	if err != nil || home != defaultHome || !strings.Contains(unit, "Environment="+systemdQuote("CODEX_HOME="+defaultHome)) {
		t.Fatalf("service must pin the effective native default: %q, %q, %v", unit, home, err)
	}
	for _, name := range []string{"CODEX_HOME", "SWITCHYARD_CODEX_HOME"} {
		t.Run(name, func(t *testing.T) {
			selected := filepath.Join(t.TempDir(), `codex % 'quoted"`)
			t.Setenv(name, selected)
			first, loginHome, err := a.runnerUnit(template, "")
			if err != nil || loginHome != selected || !strings.Contains(first, "Environment="+systemdQuote("CODEX_HOME="+selected)) {
				t.Fatalf("selected service home: %q, %q, %v", first, loginHome, err)
			}
			t.Setenv(name, "")
			t.Setenv("PATH", "/different/terminal")
			again, loginHome, err := a.runnerUnit(template, first)
			if err != nil || again != first || loginHome != selected {
				t.Fatalf("service and login must retain original home: %q, %q, %v", again, loginHome, err)
			}
			legacy := strings.Replace(first, "Environment="+systemdQuote("CODEX_HOME="+selected), "Environment="+systemdQuote("SWITCHYARD_CODEX_HOME="+selected), 1)
			migrated, loginHome, err := a.runnerUnit(template, legacy)
			if err != nil || migrated != first || loginHome != selected || migrated == legacy {
				t.Fatal("old alias units must require recreation with an explicit native home", err)
			}
			t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "different-native-home"))
			changed, _, err := a.runnerUnit(template, first)
			if err != nil || changed == first {
				t.Fatal("an explicit home change must require recreating the existing unit", err)
			}
		})
	}
}
