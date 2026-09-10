"""Opt-in real scheduler/Git acceptance, with simulated Plane and Codex app-server.

Run: python3 test/multi_project_test.py --runner /path/to/bin/symphony
Add --previous-runner /path/to/older/bin/symphony to check a warm-cache upgrade.
No paid model, external Git host, or production Plane is used.
"""
import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import shlex
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time


PROJECTS = ["11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"]
ISSUES = ["33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"]
STATES = {"55555555-5555-4555-8555-555555555555": "In Progress", "66666666-6666-4666-8666-666666666666": "Human Review"}


def git(*args, cwd=None, env=None):
    return subprocess.run(["git", *map(str, args)], cwd=cwd, env=env,
                          check=True, capture_output=True, text=True).stdout.strip()


def git_ssh():
    # Git's SSH transport invokes only these two local protocol programs.
    command = shlex.split(sys.argv[-1])
    allowed = json.loads(os.environ["SWITCHYARD_TEST_REPOS"])
    assert sys.argv[-2] == "git@switchyard-test.invalid"
    assert len(command) == 2 and command[0] in ("git-upload-pack", "git-receive-pack")
    assert command[1] in allowed
    os.execvp(command[0], command)


def app_server():
    assert "PLANE_API_KEY" not in os.environ, "Tracker token reached the app-server"

    def event(kind):
        with open(os.environ["SWITCHYARD_TEST_EVENTS"], "a") as output:
            output.write(json.dumps({"event": kind, "workspace": str(Path.cwd())}) + "\n")

    def send(value):
        print(json.dumps(value), flush=True)

    def tool(number, action, **arguments):
        send({"id": number, "method": "item/tool/call", "params": {
            "tool": "plane", "arguments": {"action": action, "issue_id": issue_id, **arguments}}})

    event("start")
    issue_id = None
    for line in sys.stdin:
        message = json.loads(line)
        method = message.get("method")
        if method == "initialize":
            send({"id": message["id"], "result": {}})
        elif method == "thread/start":
            assert any(tool["name"] == "plane" for tool in message["params"]["dynamicTools"])
            send({"id": message["id"], "result": {"thread": {"id": Path.cwd().name}}})
        elif method == "turn/start":
            prompt = message["params"]["input"][0]["text"]
            issue_id = re.search(r"Task UUID: ([0-9a-f-]{36})", prompt).group(1)
            send({"id": message["id"], "result": {"turn": {"id": "acceptance-turn"}}})
            tool(91, "read")
        elif message.get("id") in (91, 92, 93, 94):
            assert message["result"]["success"], message["result"]
            if message["id"] == 91:
                issue = json.loads(message["result"]["output"])["issue"]
                assert issue["id"] == issue_id
                assert Path("REPO.txt").read_text().strip() == issue["name"]
                tool(92, "set_state", state="In Progress")
            elif message["id"] == 92:
                # Real branch, commit and push in the checkout made by the real hooks.
                Path("RESULT.txt").write_text(issue_id + "\n")
                git("add", "RESULT.txt")
                git("commit", "-m", "Complete isolated acceptance task")
                git("push", "origin", "HEAD")
                sha = git("rev-parse", "HEAD")
                branch = git("branch", "--show-current")
                tool(93, "comment", text=f"Verified {issue_id}; commit {sha}; branch {branch}")
            elif message["id"] == 93:
                tool(94, "set_state", state="Human Review")
            else:
                event("finish")
                send({"method": "turn/completed"})
                break


def check(runner, tmp, previous_runner=None):
    source = Path(__file__).resolve().parents[1]
    repos = [tmp / "first.git", tmp / "second.git"]
    workspaces = tmp / "tasks"
    events = tmp / "events.jsonl"
    home = tmp / "home"
    home.mkdir()
    env = {"PATH": os.environ["PATH"], "HOME": str(home), "TERM": "dumb",
           "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
           "GIT_AUTHOR_NAME": "Switchyard Test", "GIT_AUTHOR_EMAIL": "test@example.invalid",
           "GIT_COMMITTER_NAME": "Switchyard Test", "GIT_COMMITTER_EMAIL": "test@example.invalid",
           "GIT_SSH_COMMAND": shlex.join([sys.executable, str(Path(__file__).resolve()), "--git-ssh"]),
           "GIT_SSH_VARIANT": "ssh", "SWITCHYARD_TEST_REPOS": json.dumps(list(map(str, repos))),
           "SWITCHYARD_TEST_EVENTS": str(events), "SWITCHYARD_ROOT": str(source),
           "SYMPHONY_WORKSPACE_ROOT": str(workspaces), "SYMPHONY_INSTALL_DIR": str(tmp / "runtime"),
           "PLANE_API_KEY": "synthetic-acceptance-key"}
    if previous_runner is not None:
        # Exercise Burrito's normal per-user cache with the same private HOME for both binaries.
        env.pop("SYMPHONY_INSTALL_DIR")

        def native_info(binary, action):
            result = subprocess.run([str(binary), "maintenance", action], cwd=tmp, env=env,
                                    check=True, capture_output=True, text=True, timeout=15)
            return result.stdout.strip()

        old_cache = Path(native_info(previous_runner, "directory"))
        new_cache = Path(native_info(runner, "directory"))
        assert old_cache.is_relative_to(home) and new_cache.is_relative_to(home), "Cache escaped the disposable HOME"
        seeded = subprocess.run([str(previous_runner)], cwd=tmp, env=env, capture_output=True, timeout=60)
        assert seeded.returncode != 0 and b"--i-understand-that-this-will-be-running-without-the-usual-guardrails" in seeded.stdout + seeded.stderr
        old_metadata = json.loads((old_cache / "_metadata.json").read_text())
        new_metadata = json.loads(native_info(runner, "meta"))
        print("Seeded previous native runtime " + old_metadata["app_version"] + "; checking candidate version " + new_metadata["app_version"], flush=True)
        assert old_cache != new_cache and old_metadata["app_version"] != new_metadata["app_version"], "Different native payloads share one Burrito cache identity"
        assert re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+\+[0-9a-f]{40}", new_metadata["app_version"]), "Native release must include its full source revision"
    urls = [f"git@switchyard-test.invalid:{repo}" for repo in repos]
    # A stale installation-wide value must never route the second project to the first repo.
    env["SOURCE_REPO_URL"] = urls[0]
    for name, repo in zip(("first", "second"), repos):
        seed = tmp / (name + "-seed")
        git("init", "--initial-branch=main", seed, env=env)
        (seed / "REPO.txt").write_text(name + "\n")
        git("add", ".", cwd=seed, env=env)
        git("commit", "-m", "Initial fixture", cwd=seed, env=env)
        git("clone", "--bare", seed, repo, env=env)

    rows = [{"id": issue, "project": project, "name": name, "sequence_id": 1,
             "state": {"name": "Todo"}, "labels": [{"name": "agent"}], "priority": "medium"}
            for issue, project, name in zip(ISSUES, PROJECTS, ("first", "second"))]
    comments = {issue: [] for issue in ISSUES}
    failures = []
    active = set()
    max_active = 0
    lock = threading.Lock()

    class Plane(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def handle_api(self):
            nonlocal max_active
            try:
                assert self.headers.get("X-API-Key") == env["PLANE_API_KEY"]
                path = self.path.split("?", 1)[0]
                match = re.fullmatch(r"/api/v1/workspaces/test/projects/([^/]+)/(.*)", path)
                assert match and match[1] in PROJECTS, path
                row = rows[PROJECTS.index(match[1])]
                resource = match[2]
                body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
                if self.command == "GET" and resource == "work-items/":
                    result = [row]
                elif self.command == "GET" and resource == "states/":
                    result = [{"id": key, "name": value} for key, value in STATES.items()]
                elif resource == f'work-items/{row["id"]}/comments/':
                    if self.command == "POST":
                        comments[row["id"]].append(body["comment_html"])
                        result = {"id": f'comment-{row["id"]}'}
                    else:
                        assert self.command == "GET"
                        result = []
                elif resource == f'work-items/{row["id"]}/':
                    if self.command == "PATCH":
                        row["state"]["name"] = STATES[body["state"]]
                        if row["state"]["name"] == "In Progress":
                            active.add(row["id"])
                            max_active = max(max_active, len(active))
                        else:
                            active.discard(row["id"])
                    else:
                        assert self.command == "GET"
                    result = row
                elif self.command == "GET" and re.fullmatch(r"work-items/[0-9a-f-]{36}/", resource):
                    # Looking up a raw UUID in another configured project is a valid miss.
                    self.send_response(404)
                    self.end_headers()
                    return
                else:
                    raise AssertionError(f"Unexpected {self.command} {path}")
                data = json.dumps(result).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
            except Exception as error:
                failures.append(str(error))
                self.send_response(500)
                self.end_headers()

        def do_GET(self):
            with lock:
                self.handle_api()

        do_POST = do_GET
        do_PATCH = do_GET

    board = ThreadingHTTPServer(("127.0.0.1", 0), Plane)
    threading.Thread(target=board.serve_forever, daemon=True).start()
    with socket.socket() as port:
        port.bind(("127.0.0.1", 0))
        dashboard_port = port.getsockname()[1]
    projects = [{"project_id": project, "project_identifier": prefix, "repo": url}
                for project, prefix, url in zip(PROJECTS, ("APP", "WEB"), urls)]
    app_command = shlex.join([sys.executable, str(Path(__file__).resolve()), "--app-server"])
    workflow = tmp / "WORKFLOW.md"
    workflow.write_text(f'''---
tracker:
  kind: plane
  provider:
    endpoint: http://127.0.0.1:{board.server_port}
    workspace: test
    projects: {json.dumps(projects)}
    api_key: $PLANE_API_KEY
  required_labels: [agent]
  active_states: [Todo, In Progress]
  terminal_states: [Done, Cancelled]
polling:
  interval_ms: 100
workspace:
  root: {json.dumps(str(workspaces))}
hooks:
  after_create: '"$SWITCHYARD_ROOT/scripts/task-workspace" init'
  before_run: '"$SWITCHYARD_ROOT/scripts/task-workspace" check'
agent:
  max_concurrent_agents: 1
  max_turns: 1
codex:
  command: {json.dumps(app_command)}
server:
  host: 127.0.0.1
  port: {dashboard_port}
---
Task UUID: {{{{ issue.id }}}}
''')
    log = tmp / "runner.log"
    process = None
    try:
        with log.open("w") as output:
            process = subprocess.Popen([str(runner), str(workflow), "--logs-root", str(tmp / "logs"),
                                       "--i-understand-that-this-will-be-running-without-the-usual-guardrails"],
                                      cwd=tmp, env=env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
            deadline = time.monotonic() + 90
            while time.monotonic() < deadline:
                assert process.poll() is None, "Runner exited before completing both tasks"
                assert not failures, failures
                if events.exists() and events.read_text().count('"event": "finish"') == 2:
                    break
                time.sleep(0.1)
            else:
                raise AssertionError("Timed out waiting for both projects to reach Human Review")
            assert [row["state"]["name"] for row in rows] == ["Human Review", "Human Review"]
            assert max_active == 1 and not active, "Shared concurrency budget was exceeded"
            concurrent = 0
            for event in map(json.loads, events.read_text().splitlines()):
                concurrent += 1 if event["event"] == "start" else -1
                assert concurrent in (0, 1), "App-server sessions overlapped with concurrency one"
            assert concurrent == 0
            for repo, project, issue, prefix, url in zip(repos, PROJECTS, ISSUES, ("APP", "WEB"), urls):
                branch = "agent/" + prefix + "-1"
                checkout = workspaces / (prefix + "-1")
                assert git("show", branch + ":RESULT.txt", cwd=repo, env=env) == issue
                assert set(git("for-each-ref", "--format=%(refname:short)", "refs/heads", cwd=repo, env=env).splitlines()) == {"main", branch}
                assert git("branch", "--show-current", cwd=checkout, env=env) == branch
                assert git("remote", "get-url", "origin", cwd=checkout, env=env) == url
                assert git("config", "--local", "--get", "switchyard.issue-id", cwd=checkout, env=env) == issue
                assert git("config", "--local", "--get", "switchyard.project-id", cwd=checkout, env=env) == project
                sha = git("rev-parse", branch, cwd=repo, env=env)
                assert len(comments[issue]) == 1 and sha in comments[issue][0] and branch in comments[issue][0]
        if previous_runner is not None:
            assert json.loads((new_cache / "_metadata.json").read_text())["app_version"] == new_metadata["app_version"]
            print("Warm-cache native upgrade selected and executed the new release successfully.")
        print("Two projects, isolated Git branches/pushes, scoped comments, Human Review and shared concurrency passed (simulated Plane/Codex).")
    except Exception:
        print(log.read_text()[-12000:] if log.exists() else "Runner did not start", file=sys.stderr)
        raise
    finally:
        if process is not None and process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
        board.shutdown()
        board.server_close()


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--git-ssh":
        git_ssh()
    elif len(sys.argv) > 1 and sys.argv[1] == "--app-server":
        app_server()
    else:
        parser = argparse.ArgumentParser(description=__doc__)
        parser.add_argument("--runner", type=Path)
        parser.add_argument("--previous-runner", type=Path, help="Older native executable to seed the same disposable user cache")
        args = parser.parse_args()
        if args.previous_runner is not None and args.runner is None:
            parser.error("--previous-runner requires --runner")
        if args.runner is None:
            print("Multi-project scheduler acceptance skipped; pass --runner to use an isolated real runner.")
        else:
            with tempfile.TemporaryDirectory(prefix="switchyard-multi-project-") as directory:
                check(args.runner.resolve(), Path(directory), args.previous_runner.resolve() if args.previous_runner else None)
