package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Kthom1/switchyard/cmd"
	"github.com/Kthom1/switchyard/config"
)

func TestLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("the release targets Linux x86-64")
	}
	base := t.TempDir()
	root, bin := filepath.Join(base, `installation with 'quotes"`), filepath.Join(base, "path")
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("SWITCHYARD_HOME", root)
	t.Setenv("GH_CONFIG_DIR", filepath.Join(base, "gh"))
	t.Setenv("CODEX_HOME", filepath.Join(base, "personal-codex"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("PATH", bin)
	t.Setenv("TEST_CALLS", filepath.Join(base, "calls"))
	t.Setenv("TEST_FAIL", filepath.Join(base, "fail-compose"))
	t.Setenv("TEST_INACTIVE", "")
	write := func(path, content string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	run := func(wantError string, args ...string) string {
		t.Helper()
		output, err := os.CreateTemp(base, "stdout-*")
		if err != nil {
			t.Fatal(err)
		}
		previous := os.Stdout
		os.Stdout = output
		defer func() { os.Stdout = previous; output.Close() }()
		err = cmd.Execute(args, assets, version)
		if wantError == "" && err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if wantError != "" && (err == nil || (wantError != "*" && !strings.Contains(err.Error(), wantError))) {
			t.Fatalf("%v: got %v, want error containing %q", args, err, wantError)
		}
		return read(output.Name())
	}
	run("", "--help")
	if output := run("", "init", "--help"); strings.Contains(output, "-workspace") || strings.Contains(output, "-project-id") || strings.Contains(output, "-api-key-stdin") {
		t.Fatal("init help must describe installation settings only")
	}
	run("choose either --recommended or --clean", "init", "--recommended", "--clean")
	run("choose two different ports", "init", "--port", "8091")
	for _, name := range []string{"--repo", "--workspace", "--project-id", "--identifier", "--api-key-stdin"} {
		run("flag provided but not defined", "init", name, "example")
	}
	run("install prerequisites first: bash, docker, git, gh, codex, systemctl, journalctl", "init")
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("missing prerequisites should leave no installation: %v", err)
	}

	// Only these real local tools can run. Network, auth and service CLIs are stubs.
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bash", "git", "dirname", "mkdir", "env"} {
		if err := os.Symlink(filepath.Join("/usr/bin", name), filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	stub := `#!/bin/sh
name=${0##*/}
printf '%s' "$name" >> "$TEST_CALLS"
printf ' <%s>' "$@" >> "$TEST_CALLS"
printf '\n' >> "$TEST_CALLS"
if [ "$name" = systemctl ] && [ "$2" = is-active ] && [ -n "$TEST_INACTIVE" ]; then
  exit 3
fi
if [ "$name" = codex ]; then
  printf 'CODEX_HOME <%s>\n' "$CODEX_HOME" >> "$TEST_CALLS"
  case "$*" in
    'plugin list --json') printf '{"installed":[]}\n';;
    'plugin marketplace list --json') printf '{"marketplaces":[]}\n';;
  esac
fi
if [ "$name" = docker ] && [ -e "$TEST_FAIL" ]; then
  case " $* " in *" up "*) echo 'test: compose startup failed' >&2; exit 23;; esac
fi
if [ "$name" = docker ] && [ "$1" = volume ] && [ "$2" = ls ]; then
  printf '%s\n' "$TEST_VOLUME"
fi
`
	for _, name := range []string{"docker", "systemctl", "gh", "codex", "journalctl"} {
		write(filepath.Join(bin, name), stub, 0700)
	}
	runner := filepath.Join(base, "release/bin/symphony")
	write(runner, "#!/bin/sh\nexit 0\n", 0700)
	write(filepath.Join(base, "release/licenses/NOTICE"), "test release notice\n", 0600)
	var unavailable atomic.Bool
	var initialized atomic.Bool
	server := func(path string) *httptest.Server {
		return httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if path == "/api/instances/" && r.URL.Path == "/api/v1/workspaces/example/projects/12345678-1234-1234-1234-123456789abc/" {
				if r.Method != http.MethodGet || r.Header.Get("X-API-Key") != "test-plane'key$HOME" {
					t.Error("setup must verify project access with its API key")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				fmt.Fprintln(w, `{"id":"12345678-1234-1234-1234-123456789abc","identifier":"APP"}`)
				return
			}
			if r.URL.Path != path {
				t.Errorf("unexpected readiness path %q; want %q", r.URL.Path, path)
				http.NotFound(w, r)
				return
			}
			if unavailable.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			if path == "/api/instances/" {
				fmt.Fprintf(w, `{"instance":{"is_setup_done":%t}}`, initialized.Load())
			} else {
				fmt.Fprintln(w, "{}")
			}
		}))
	}
	board, dashboard := server("/api/instances/"), server("/api/v1/state")
	defer board.Close()
	defer dashboard.Close()
	port := func(s *httptest.Server) string { return fmt.Sprint(s.Listener.Addr().(*net.TCPAddr).Port) }
	initArgs := []string{"init", "--runner", runner, "--port", port(board), "--runner-port", port(dashboard)}

	for _, origin := range []string{"ftp://host", "https://user:pass@host", "https://host/path", "https://host\nINJECT=value"} {
		run("origin", append(append([]string{}, initArgs...), "--web-url", origin)...)
		for _, name := range []string{"config.json", ".env.plane"} {
			if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
				t.Fatalf("rejected origin must not persist %s: %v", name, err)
			}
		}
	}

	for _, occupied := range []struct {
		name   string
		server *httptest.Server
	}{{"board", board}, {"runner", dashboard}} {
		run(occupied.name+" port", initArgs...)
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatalf("occupied %s port must leave no installation files: %v", occupied.name, err)
		}
		occupied.server.Listener.Close()
	}
	t.Setenv("TEST_VOLUME", config.Name(root)+"_pgdata")
	run("Plane data volume "+os.Getenv("TEST_VOLUME"), initArgs...)
	for _, name := range []string{"config.json", ".env.plane"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("retained Plane data must not receive regenerated %s: %v", name, err)
		}
	}
	t.Setenv("TEST_VOLUME", "")

	// A failed first start is resumable without regenerating database secrets.
	write(os.Getenv("TEST_FAIL"), "fail\n", 0600)
	run("exit status 23", initArgs...)
	preserved := map[string]string{}
	for _, path := range []string{"config.json", ".env.plane"} {
		preserved[path] = read(filepath.Join(root, path))
	}
	plane := map[string]string{}
	for _, line := range strings.Split(preserved[".env.plane"], "\n") {
		name, value, _ := strings.Cut(line, "=")
		plane[name] = value
	}
	seen := map[string]bool{}
	for _, name := range []string{"POSTGRES_PASSWORD", "RABBITMQ_PASSWORD", "SECRET_KEY", "LIVE_SERVER_SECRET_KEY", "AWS_SECRET_ACCESS_KEY"} {
		value := plane[name]
		if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" || seen[value] {
			t.Fatalf("Plane must generate a distinct 32-byte hex secret for %s", name)
		}
		seen[value] = true
	}
	if plane["WEB_URL"] != "http://localhost:"+port(board) || plane["DATABASE_URL"] != "postgresql://plane:"+plane["POSTGRES_PASSWORD"]+"@plane-db/plane" || plane["AMQP_URL"] != "amqp://plane:"+plane["RABBITMQ_PASSWORD"]+"@plane-mq:5672/plane" {
		t.Fatal("Plane origin and connection strings must match the generated configuration")
	}
	if err := os.Remove(os.Getenv("TEST_FAIL")); err != nil {
		t.Fatal(err)
	}
	for _, server := range []*httptest.Server{board, dashboard} {
		listener, err := net.Listen("tcp", server.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		server.Listener = listener
		server.Start()
	}
	run("", initArgs...)
	preserved["plane.json"] = read(filepath.Join(root, "plane.json"))
	for _, name := range []string{".env", "WORKFLOW.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("init must leave repositories unconnected: %s", name)
		}
	}
	run("connect a repository", "up")
	write(filepath.Join(root, "workspaces/APP-1/notes.txt"), "existing task work\n", 0600)
	preserved["workspaces/APP-1/notes.txt"] = "existing task work\n"
	run("", "init")
	if output := run("", "init", "--clean"); !strings.Contains(output, "project add --repo") || strings.Contains(output, "init --clean --repo") {
		t.Fatal("Clean setup must direct repository connections to project add")
	}
	if strings.Contains(read(os.Getenv("TEST_CALLS")), "<plugin>") {
		t.Fatal("plain init must not install plugins")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("CODEX_HOME"), "skills")); !os.IsNotExist(err) {
		t.Fatal("plain init must not install skills")
	}
	// Existing skills let the opt-in route run without a network fetch.
	for _, name := range []string{"frontend-design", "show-me"} {
		write(filepath.Join(os.Getenv("CODEX_HOME"), "skills", name, "SKILL.md"), "keep "+name, 0600)
	}
	if output := run("", "init", "--recommended"); !strings.Contains(output, "project add --repo") || strings.Contains(output, "init --recommended --repo") {
		t.Fatal("Recommended setup must direct repository connections to project add")
	}
	for _, name := range []string{"frontend-design", "show-me"} {
		if read(filepath.Join(os.Getenv("CODEX_HOME"), "skills", name, "SKILL.md")) != "keep "+name {
			t.Fatal("recommended setup must preserve existing configured Codex skills")
		}
	}
	callsAfterInstall := read(os.Getenv("TEST_CALLS"))
	for _, name := range []string{"ponytail@ponytail", "compound-engineering@compound-engineering-plugin"} {
		if !strings.Contains(callsAfterInstall, "<plugin> <add> <"+name+">") {
			t.Fatalf("recommended setup did not install %s", name)
		}
	}
	// A manually populated board needs no generated local ownership credentials.
	initialized.Store(true)
	if err := os.Remove(filepath.Join(root, "plane.json")); err != nil {
		t.Fatal(err)
	}
	setupCalls := strings.Count(read(os.Getenv("TEST_CALLS")), "<manage.py> <shell>")
	if output := run("", "init", "--clean"); !strings.Contains(output, "Existing Plane accounts are preserved") {
		t.Fatal("manual board setup must explain the existing-project connection path")
	}
	if _, err := os.Stat(filepath.Join(root, "plane.json")); !os.IsNotExist(err) {
		t.Fatal("init must not mint credentials or claim an existing manual board")
	}
	write(filepath.Join(root, "plane.json"), "invalid saved metadata", 0600)
	run("invalid plane.json", "init", "--clean")
	if read(filepath.Join(root, "plane.json")) != "invalid saved metadata" {
		t.Fatal("existing metadata must be validated and preserved")
	}
	if err := os.Remove(filepath.Join(root, "plane.json")); err != nil {
		t.Fatal(err)
	}

	runWithKey := func(key, wantError string, args ...string) {
		t.Helper()
		inputPath := filepath.Join(base, "key")
		write(inputPath, key, 0600)
		input, err := os.Open(inputPath)
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		oldStdin := os.Stdin
		os.Stdin = input
		defer func() { os.Stdin = oldStdin }()
		run(wantError, args...)
	}
	run("provide --repo", "project", "add", "--workspace", "example")
	run("", "project", "add", "--help")
	runnerArgs := []string{"project", "add", "--repo", "https://github.com/example/project", "--workspace", "example", "--project-id", "12345678-1234-1234-1234-123456789abc", "--identifier", "APP", "--api-key-stdin"}
	run("use --api-key-stdin", runnerArgs[:len(runnerArgs)-1]...)
	assertUnconfigured := func() {
		t.Helper()
		for _, name := range []string{".env", "WORKFLOW.md"} {
			if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
				t.Fatalf("rejected runner input must not persist %s: %v", name, err)
			}
		}
	}
	for _, invalid := range [][2]string{
		{"--repo", "file:///tmp/repository"},
		{"--repo", "https://user:password@github.com/example/project"},
		{"--repo", "https://github.com/example/project?token=hidden"},
		{"--workspace", "../example"},
		{"--project-id", "not-a-uuid"},
		{"--identifier", "lowercase"},
	} {
		args := append([]string{}, runnerArgs...)
		for i := range args {
			if args[i] == invalid[0] {
				args[i+1] = invalid[1]
			}
		}
		runWithKey("test-plane-key\n", "*", args...)
		assertUnconfigured()
	}
	sshArgs := append([]string{}, runnerArgs...)
	sshArgs[3] = "git@github.com:example/project.git"
	runWithKey("bad key\n", "API key", sshArgs...)
	assertUnconfigured()
	for _, invalidKey := range []string{"", "\n", "bad key\n", "bad\tkey\n"} {
		runWithKey(invalidKey, "*", runnerArgs...)
		assertUnconfigured()
	}
	key := "test-plane'key$HOME"
	runWithKey(key+"\n", "", runnerArgs...)
	if strings.Count(read(os.Getenv("TEST_CALLS")), "<manage.py> <shell>") != setupCalls {
		t.Fatal("manual board init and project add must not provision accounts")
	}
	if _, err := os.Stat(filepath.Join(root, "plane.json")); !os.IsNotExist(err) {
		t.Fatal("manual project connection must not mint local ownership credentials")
	}
	write(filepath.Join(root, "plane.json"), preserved["plane.json"], 0600)
	for _, path := range []string{".env", "WORKFLOW.md"} {
		preserved[path] = read(filepath.Join(root, path))
	}
	for _, path := range []string{".env.plane", ".env", "config.json", "WORKFLOW.md"} {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s must have mode 0600: %v, %v", path, info, err)
		}
	}
	loaded, err := exec.Command("bash", "-c", `source "$1"; printf '%s\n' "$PLANE_API_KEY" "$SOURCE_REPO_URL" "$SYMPHONY_WORKSPACE_ROOT"`, "test", filepath.Join(root, ".env")).CombinedOutput()
	wantEnvironment := key + "\n\n" + filepath.Join(root, "workspaces") + "\n"
	if err != nil || string(loaded) != wantEnvironment {
		t.Fatalf("runner environment must preserve literal shell values: %v\n%s", err, loaded)
	}
	for _, want := range []string{
		`endpoint: ` + board.URL,
		`web_url: http://localhost:` + port(board),
		`workspace: example`,
		`project_id: 12345678-1234-1234-1234-123456789abc`,
		`project_identifier: APP`,
		`repo: https://github.com/example/project`,
		"port: " + port(dashboard),
	} {
		if !strings.Contains(preserved["WORKFLOW.md"], want) {
			t.Errorf("runner workflow is missing %q", want)
		}
	}
	if strings.Contains(preserved["WORKFLOW.md"], "REPLACE_WITH") {
		t.Fatal("runner workflow contains an unresolved placeholder")
	}
	if output := run("", "project", "list"); !strings.Contains(output, "APP") || !strings.Contains(output, "https://github.com/example/project") {
		t.Fatal("project list must show the saved mapping")
	}
	run("", "project", "add", "--repo", "https://github.com/example/project")
	run("different repository", "project", "add", "--repo", "https://github.com/example/wrong", "--project-id", "12345678-1234-1234-1234-123456789abc", "--identifier", "APP")
	run("", "init")
	t.Setenv("TEST_INACTIVE", "1")
	run("already in use", "up")
	if strings.Contains(read(os.Getenv("TEST_CALLS")), "<enable>") {
		t.Fatal("an unrelated listener must not count as a ready runner")
	}
	t.Setenv("TEST_INACTIVE", "")
	run("", "up")
	unitPath, err := config.UnitPath(root)
	if err != nil {
		t.Fatal(err)
	}
	savedUnit := read(unitPath)
	t.Setenv("PATH", bin+":/nonexistent")
	run("", "up")
	if read(unitPath) != savedUnit {
		t.Fatal("a different shell PATH must preserve the existing service")
	}
	changedUnit := strings.Replace(savedUnit, "KillMode=control-group", "KillMode=process", 1)
	write(unitPath, changedUnit, 0600)
	run("service configuration differs", "up")
	if read(unitPath) != changedUnit {
		t.Fatal("a conflicting service must not be overwritten")
	}
	write(unitPath, savedUnit, 0600)
	t.Setenv("PATH", bin)
	run("", "status")
	run("", "logs", "plane", "--follow")
	run("", "logs", "runner")
	run("usage: switchyard logs", "logs", "invalid")
	run("", "codex", "login", "--device-auth")
	unavailable.Store(true)
	run("board is unavailable; run switchyard logs plane", "status")
	unavailable.Store(false)
	run("", "status")
	run("", "down")
	for path, want := range preserved {
		if got := read(filepath.Join(root, path)); got != want {
			t.Errorf("lifecycle changed preserved file %s", path)
		}
	}
	calls := read(os.Getenv("TEST_CALLS"))
	for _, want := range []string{
		"docker <compose> <version>", "gh <auth> <status>",
		"docker <compose> <--project-name> <" + config.Name(root) + "> <--env-file> <" + filepath.Join(root, ".env.plane") + ">",
		"<-f> <deploy/docker-compose.yml> <-f> <deploy/compose.override.yml> <up> <-d>",
		"<ps> <--all>", "<logs> <--tail> <100> <--follow>", "<deploy/compose.override.yml> <down>\n",
		"systemctl <--user> <enable> <--now> <" + config.Name(root) + ".service>",
		"systemctl <--user> <disable> <--now> <" + config.Name(root) + ".service>",
		"journalctl <--user> <-u> <" + config.Name(root) + ".service> <--no-pager> <-n> <100>",
		"codex <login> <status>",
		"codex <login> <--device-auth>",
		"CODEX_HOME <" + os.Getenv("CODEX_HOME") + ">",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing command route %q in:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, key) || strings.Contains(calls, "<--volumes>") || strings.Contains(calls, "<-v>") {
		t.Fatal("commands exposed the API key or requested volume deletion")
	}
	escapedRoot := strings.ReplaceAll(root, `"`, `\"`)
	unit := read(unitPath)
	for _, line := range []string{`WorkingDirectory=` + strings.ReplaceAll(root, "%", "%%") + `/.`, `ExecStart=:/usr/bin/env "` + escapedRoot + `/scripts/run"`} {
		if !strings.Contains(unit, line+"\n") {
			t.Fatalf("unit must preserve the quote in the installation path; missing %q in:\n%s", line, unit)
		}
	}
	if _, err := os.Stat("/usr/bin/systemd-analyze"); err == nil {
		cmd := exec.Command("/usr/bin/systemd-analyze", "--system", "--man=no", "--generators=no", "verify", unitPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generated service must pass systemd's parser: %v\n%s", err, output)
		}
	}
}
