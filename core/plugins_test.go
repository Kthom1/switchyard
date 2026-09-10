package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pluginInstallation(t *testing.T) (Installation, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	t.Setenv("SWITCHYARD_CODEX_HOME", "")
	t.Setenv("TEST_CODEX_PLUGINS", `[]`)
	t.Setenv("TEST_CODEX_MARKETPLACES", `[]`)
	t.Setenv("PATH", t.TempDir())
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "commands")
	t.Setenv("RECOMMENDED_COMMAND_LOG", log)
	stub := `#!/bin/sh
printf '%s\n' "codex ${CODEX_HOME:-${SWITCHYARD_CODEX_HOME:-$HOME/.codex}} $*" >> "$RECOMMENDED_COMMAND_LOG"
case "$*" in
  'plugin list --json') printf '{"installed":%s}\n' "$TEST_CODEX_PLUGINS" ;;
  'plugin marketplace list --json') printf '{"marketplaces":%s}\n' "$TEST_CODEX_MARKETPLACES" ;;
esac
`
	if err := os.WriteFile(filepath.Join(scripts, "codex-runner"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	return Installation{Root: root}, log
}

func TestRecommendedInstallsAllFourInSelectedNativeHome(t *testing.T) {
	a, log := pluginInstallation(t)
	home := filepath.Join(t.TempDir(), "configured-codex")
	t.Setenv("CODEX_HOME", home)
	git := `#!/bin/sh
printf '%s\n' "git $*" >> "$RECOMMENDED_COMMAND_LOG"
checkout=$2
shift 2
case "$*" in
  '-c core.hooksPath=/dev/null checkout --quiet FETCH_HEAD -- skills/frontend-design')
    /bin/mkdir -p "$checkout/skills/frontend-design"
    printf '%s' frontend > "$checkout/skills/frontend-design/SKILL.md"
    printf '%s' frontend-license > "$checkout/skills/frontend-design/LICENSE.txt"
    ;;
  '-c core.hooksPath=/dev/null checkout --quiet FETCH_HEAD -- plugins/show-me/skills/show-me LICENSE')
    /bin/mkdir -p "$checkout/plugins/show-me/skills/show-me"
    printf '%s' show-me > "$checkout/plugins/show-me/skills/show-me/SKILL.md"
    printf '%s' show-me-license > "$checkout/LICENSE"
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "git"), []byte(git), 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallRecommended(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	commands := string(data)
	codex := "codex " + home + " "
	for _, command := range []string{
		codex + "plugin marketplace add DietrichGebert/ponytail --ref 356918eba965ee1eac64bd3a7f0dd02108350de5\n",
		codex + "plugin add ponytail@ponytail\n",
		codex + "plugin marketplace add EveryInc/compound-engineering-plugin --ref 8df67793b9733d2220fa9a7fc37139931471af62\n",
		codex + "plugin add compound-engineering@compound-engineering-plugin\n",
		"fetch --quiet --depth 1 https://github.com/anthropics/skills.git 41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f\n",
		"fetch --quiet --depth 1 https://github.com/humanlayer/skills.git 3c2629142c5d437428269b1b722b08c0b87f574d\n",
	} {
		if !strings.Contains(commands, command) {
			t.Fatalf("missing command %q in %s", command, commands)
		}
	}
	if strings.Count(commands, "\n") != 12 {
		t.Fatalf("unexpected commands: %s", commands)
	}
	for path, want := range map[string]string{
		"frontend-design/SKILL.md":    "frontend",
		"frontend-design/LICENSE.txt": "frontend-license",
		"show-me/SKILL.md":            "show-me",
		"show-me/LICENSE":             "show-me-license",
	} {
		data, err := os.ReadFile(filepath.Join(home, "skills", path))
		if err != nil || string(data) != want {
			t.Fatalf("installed %s: %q, %v", path, data, err)
		}
	}
}

func TestRecommendedPreservesNativeAndUserSkills(t *testing.T) {
	for _, inheritedName := range []string{"frontend-design", "show-me"} {
		t.Run(inheritedName, func(t *testing.T) {
			a, log := pluginInstallation(t)
			for _, name := range []string{"frontend-design", "show-me"} {
				path := filepath.Join(os.Getenv("HOME"), ".codex/skills", name)
				if name == inheritedName {
					path = filepath.Join(os.Getenv("HOME"), ".agents/skills", name)
				}
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("keep "+name), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.InstallRecommended(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil || strings.Count(string(data), "\n") != 6 {
				t.Fatalf("unexpected commands: %s, %v", data, err)
			}
			for _, name := range []string{"frontend-design", "show-me"} {
				path := filepath.Join(os.Getenv("HOME"), ".codex/skills", name)
				if name == inheritedName {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatal("duplicated inherited skill", name)
					}
					path = filepath.Join(os.Getenv("HOME"), ".agents/skills", name)
				}
				data, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
				if err != nil || string(data) != "keep "+name {
					t.Fatalf("existing %s changed: %q, %v", name, data, err)
				}
			}
		})
	}
}

func TestRecommendedPreservesInstalledPluginsAndConfiguredMarketplaces(t *testing.T) {
	a, log := pluginInstallation(t)
	t.Setenv("TEST_CODEX_PLUGINS", `[{"pluginId":"ponytail@ponytail","enabled":false}]`)
	t.Setenv("TEST_CODEX_MARKETPLACES", `[{"name":"compound-engineering-plugin"}]`)
	for _, name := range []string{"frontend-design", "show-me"} {
		path := filepath.Join(os.Getenv("HOME"), ".codex/skills", name)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("existing"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.InstallRecommended(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil || strings.Count(string(data), "\n") != 3 || strings.Contains(string(data), "marketplace add") || strings.Contains(string(data), "plugin add ponytail") {
		t.Fatalf("existing plugins or marketplaces were changed: %s, %v", data, err)
	}
	if !strings.Contains(string(data), "plugin add compound-engineering@compound-engineering-plugin") {
		t.Fatal("missing plugin was not installed from existing marketplace")
	}
}

func TestSkillCopyPublishesCompleteSkillAndRejectsUnsafeSources(t *testing.T) {
	source := t.TempDir()
	target := filepath.Join(t.TempDir(), "skills/show-me")
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill"), 0600); err != nil {
		t.Fatal(err)
	}
	license := filepath.Join(t.TempDir(), "LICENSE")
	if err := os.WriteFile(license, []byte("license"), 0600); err != nil {
		t.Fatal(err)
	}
	linkedSource := filepath.Join(t.TempDir(), "linked-source")
	if err := os.Symlink(source, linkedSource); err != nil {
		t.Fatal(err)
	}
	if err := copySkill(linkedSource, target, license); err == nil {
		t.Fatal("symlink source directory accepted")
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(source, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := copySkill(source, target, license); err == nil {
		t.Fatal("symlink source accepted")
	}
	if err := os.Remove(filepath.Join(source, "outside")); err != nil {
		t.Fatal(err)
	}
	linkedLicense := filepath.Join(t.TempDir(), "linked-license")
	if err := os.Symlink(license, linkedLicense); err != nil {
		t.Fatal(err)
	}
	if err := copySkill(source, target, linkedLicense); err == nil {
		t.Fatal("symlink license accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("failed copy published a partial skill")
	}
	if err := copySkill(source, target, license); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"SKILL.md": "skill", "LICENSE": "license"} {
		data, err := os.ReadFile(filepath.Join(target, name))
		if err != nil || string(data) != want {
			t.Fatalf("published %s: %q, %v", name, data, err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copySkill(source, target, license); err == nil {
		t.Fatal("existing skill overwritten")
	}
	if data, err := os.ReadFile(filepath.Join(target, "SKILL.md")); err != nil || string(data) != "skill" {
		t.Fatalf("existing skill changed: %q, %v", data, err)
	}
}
