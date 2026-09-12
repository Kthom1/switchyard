"""Opt-in fresh Plane provisioning check: python3 test/local_setup_test.py --cli work/switchyard."""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import shlex
import socket
import subprocess
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import uuid


def failure_summary(result, env):
    # Classify known failures without publishing captured output or private paths.
    output = (result.stdout + result.stderr).lower()
    categories = (
        (b"toomanyrequests", "registry rate limit"),
        (b"too many requests", "registry rate limit"),
        (b"manifest unknown", "image manifest unavailable"),
        (b"no matching manifest", "image platform unavailable"),
        (b"unauthorized", "authorization failure"),
        (b"pull access denied", "image access denied"),
        (b"error getting credentials", "Docker credential helper"),
        (b"cannot connect to the docker daemon", "Docker daemon unavailable"),
        (b"'compose' is not a docker command", "Compose unavailable"),
        (b"timeout", "timeout"),
        (b"no such host", "DNS failure"),
        (b"already in use", "port unavailable"),
        (b"permission denied", "permission denied"),
        (b"no space left on device", "disk full"),
        (b"install prerequisites first", "missing prerequisite"),
        (b"a systemd user session is required", "systemd session unavailable"),
    )
    category = next((label for text, label in categories if text in output), "unclassified")
    root = Path(env["SWITCHYARD_HOME"])
    artifacts = [name for name in ("scripts/plane", "bin/symphony", "config.json", ".env.plane", "plane.json")
                 if (root / name).is_file()]
    return f"category={category}; prepared={','.join(artifacts) or 'none'}"


def run(args, env, *, check=True, data=None):
    result = subprocess.run([str(arg) for arg in args], env=env, capture_output=True,
                            input=data, stdin=subprocess.DEVNULL if data is None else None)
    if check and result.returncode:
        # Setup output can contain credentials if the implementation regresses.
        raise RuntimeError(f"{Path(args[0]).name} failed with exit {result.returncode}; "
                           f"{failure_summary(result, env)}; output withheld")
    return result


def check(cli, tmp):
    root, home, binaries = tmp / "installation's home", tmp / "user", tmp / "bin"
    home.mkdir()
    binaries.mkdir()
    project = "switchyard-" + hashlib.sha256(os.fsencode(root.resolve())).hexdigest()[:12]
    marker = tmp / "unexpected-agent-or-service-command"
    stub = "#!/bin/sh\ncase \"${0##*/} $*\" in\n  'gh auth status'|'systemctl --user show-environment') exit 0;;\nesac\nprintf unexpected > " + shlex.quote(str(marker)) + "\nexit 99\n"
    for name in ("gh", "codex", "systemctl", "journalctl"):
        path = binaries / name
        path.write_text(stub)
        path.chmod(0o700)
    bundle = tmp / "runner bundle"
    (bundle / "bin").mkdir(parents=True)
    (bundle / "licenses").mkdir()
    (bundle / "licenses/NOTICE").write_text("Synthetic runner; this test must never dispatch work.\n")
    runner = bundle / "bin/symphony"
    runner.write_text("#!/bin/sh\nprintf unexpected > " + shlex.quote(str(marker)) + "\nexit 99\n")
    runner.chmod(0o700)
    # Keep the two sockets open together so the chosen ports differ.
    with socket.socket() as board_socket, socket.socket() as runner_socket:
        board_socket.bind(("127.0.0.1", 0))
        runner_socket.bind(("127.0.0.1", 0))
        board_port, runner_port = board_socket.getsockname()[1], runner_socket.getsockname()[1]
    origin = f"http://127.0.0.1:{board_port}"
    env = {name: value for name, value in os.environ.items()
           if name in ("PATH", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH")}
    env.update(HOME=str(home), PATH=str(binaries) + os.pathsep + os.environ["PATH"],
               SWITCHYARD_HOME=str(root), SWITCHYARD_COMPOSE_PROJECT=project,
               SWITCHYARD_PLANE_PORT=str(board_port))
    init = [cli, "init", "--clean", "--runner", runner, "--port", board_port,
            "--runner-port", runner_port, "--web-url", origin]
    connect = [cli, "project", "add", "--repo", "https://github.com/example/project"]
    plain = urllib.request.build_opener()
    legacy_identity = False

    def request(opener, path, *, headers=None, data=None):
        req = urllib.request.Request(origin + path, headers=headers or {}, data=data)
        try:
            with opener.open(req, timeout=15) as response:
                assert response.url.startswith(origin + "/"), "Plane redirected outside the disposable instance"
                return response.status, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.read()

    def get_json(opener, path, headers=None):
        status, body = request(opener, path, headers=headers)
        assert status == 200, f"Unexpected HTTP {status} from {path}"
        return json.loads(body)

    def login(settings):
        browser = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        csrf = get_json(browser, "/auth/get-csrf-token/")["csrf_token"]
        status, _ = request(browser, "/auth/sign-in/",
                            headers={"X-CSRFToken": csrf, "Referer": origin + "/", "Content-Type": "application/x-www-form-urlencoded"},
                            data=urllib.parse.urlencode({"email": settings["owner_email"], "password": settings["owner_password"]}).encode())
        assert status == 200, "Owner sign-in failed"
        user = get_json(browser, "/api/users/me/")
        assert user["id"] == settings["owner_id"] and user["email"] == settings["owner_email"]
        return browser

    def public_api(settings):
        headers = {"X-API-Key": settings["api_key"]}
        base = f'/api/v1/workspaces/{settings["workspace"]}/projects/{settings["project_id"]}/'
        selected = get_json(plain, base, headers)
        assert selected["id"] == settings["project_id"] and selected["identifier"] == settings["identifier"]
        data = {}
        for resource, names in (("states", {"Backlog", "Todo", "In Progress", "Human Review", "Blocked", "Done", "Cancelled"}), ("labels", {"agent"})):
            values = get_json(plain, base + resource + "/", headers)
            values = values if isinstance(values, list) else values["results"]
            assert len(values) == len(names) and {value["name"] for value in values} == names, resource
            data[resource] = sorted((value["id"], value["name"]) for value in values)
        status, _ = request(plain, "/api/instances/admins/me/", headers=headers)
        assert status in (401, 403), "Automation API key obtained instance admin access"
        return data

    def database_state(settings):
        code = f"""import json
from plane.db.models import User, Workspace, WorkspaceMember, Project, ProjectMember, APIToken, Label
from plane.license.models import Instance, InstanceAdmin
owner = User.objects.get(pk={settings['owner_id']!r})
agent = User.objects.get(pk={settings['automation_id']!r})
assert agent.is_bot is {not legacy_identity!r} and not agent.is_staff and not agent.is_superuser and not agent.has_usable_password()
assert not InstanceAdmin.objects.filter(user=agent).exists()
assert InstanceAdmin.objects.filter(user=owner, role=20).exists()
assert WorkspaceMember.objects.get(workspace_id={settings['workspace_id']!r}, member=agent).role == 15
assert ProjectMember.objects.get(project_id={settings['project_id']!r}, member=agent).role == 15
assert APIToken.objects.filter(user=agent, workspace_id={settings['workspace_id']!r}).count() == 1
assert APIToken.objects.get(user=agent, workspace_id={settings['workspace_id']!r}).user_type == {0 if legacy_identity else 1}
print(json.dumps({{model.__name__: list(model.objects.order_by('id').values_list('id', flat=True)) for model in (User, Workspace, WorkspaceMember, Project, ProjectMember, APIToken, Label, Instance, InstanceAdmin)}}, default=str))
"""
        result = run([root / "scripts/plane", "exec", "-T", "api", "python", "manage.py", "shell", "-c", code], env)
        return json.loads(result.stdout.splitlines()[-1])

    def no_secrets(result, settings):
        for name in ("owner_password", "api_key"):
            assert settings[name].encode() not in result.stdout + result.stderr, "Setup printed a credential"

    try:
        print("Checking fresh local setup and authenticated access", flush=True)
        first = run(init, env)
        settings = json.loads((root / "plane.json").read_text())
        assert settings["workspace"] == "switchyard" and settings["identifier"] == "APP"
        assert settings["owner_email"] == "owner@switchyard.local"
        no_secrets(first, settings)
        assert not (root / ".env").exists() and not (root / "WORKFLOW.md").exists(), "Init connected a repository"
        assert b"No repositories connected" in run([cli, "project", "list"], env).stdout
        board_files = ("plane.json", "config.json", ".env.plane")
        board_saved = {name: (root / name).read_bytes() for name in board_files}
        repeated_board = run(init, env)
        no_secrets(repeated_board, settings)
        assert board_saved == {name: (root / name).read_bytes() for name in board_files}
        assert not (root / ".env").exists() and not (root / "WORKFLOW.md").exists()
        print("Checking first repository connection through project add", flush=True)
        connected = run(connect, env)
        no_secrets(connected, settings)
        assert board_saved == {name: (root / name).read_bytes() for name in board_files}, "First connection changed board identity"
        files = ("plane.json", "config.json", ".env.plane", ".env", "WORKFLOW.md")
        saved = {name: (root / name).read_bytes() for name in files}
        for name in files:
            assert (root / name).stat().st_mode & 0o777 == 0o600, name
        assert settings["api_key"].encode() in saved[".env"]
        assert settings["owner_password"].encode() not in saved[".env"]
        login(settings)
        api_state = public_api(settings)
        db_state = database_state(settings)
        assert len(db_state["User"]) == 2 and len(db_state["APIToken"]) == 1
        print("Checking repeated configured init", flush=True)
        second = run(init, env)
        no_secrets(second, settings)
        assert saved == {name: (root / name).read_bytes() for name in files}, "Repeated init changed private configuration"
        assert database_state(settings) == db_state, "Repeated init duplicated or replaced Plane records"
        assert public_api(settings) == api_state
        login(settings)
        assert not marker.exists(), "Init launched an agent or changed a systemd service"
        assert not (home / ".config/systemd/user" / (project + ".service")).exists()

        print("Checking bootstrap replay with preserved local ownership", flush=True)
        for name in (".env", "WORKFLOW.md"):
            (root / name).unlink()
        replay = run(init, env)
        no_secrets(replay, settings)
        assert not (root / ".env").exists() and not (root / "WORKFLOW.md").exists(), "Init recreated a repository connection"
        run(connect, env)
        assert saved == {name: (root / name).read_bytes() for name in files}, "Bootstrap replay changed private configuration"
        assert database_state(settings) == db_state, "Bootstrap replay changed Plane identities"
        assert public_api(settings) == api_state
        login(settings)

        print("Checking another repository on the same board", flush=True)
        base = f'/api/v1/workspaces/{settings["workspace"]}/projects/{settings["project_id"]}/'
        headers = {"X-API-Key": settings["api_key"], "Content-Type": "application/json"}
        status, body = request(plain, base + "work-items/", headers=headers,
                               data=json.dumps({"name": "First project retains this task", "description_html": "<p>Existing project data.</p>"}).encode())
        assert status == 201, f"Task creation failed with HTTP {status}"
        task = json.loads(body)
        task_path = base + "work-items/" + task["id"] + "/"
        task_before = get_json(plain, task_path, headers)
        add = [cli, "project", "add", "--repo", "https://github.com/example/second"]
        added = run(add, env)
        no_secrets(added, settings)
        lines = run([cli, "project", "list"], env).stdout.decode().splitlines()
        mappings = [line.split() for line in lines[1:]]
        assert len(mappings) == 2 and mappings[0] == ["APP", settings["project_id"], "https://github.com/example/project"]
        assert mappings[1][0] == "SECOND" and mappings[1][2] == "https://github.com/example/second"
        second_settings = {**settings, "project_id": mappings[1][1], "identifier": "SECOND"}
        second_api = public_api(second_settings)
        multi_state = database_state(second_settings)
        assert len(multi_state["Workspace"]) == 1 and len(multi_state["Project"]) == 2
        assert len(multi_state["User"]) == 2 and len(multi_state["APIToken"]) == 1
        assert get_json(plain, task_path, headers) == task_before, "Adding a project changed existing task data"
        for name in files[:-1]:
            assert (root / name).read_bytes() == saved[name], "Adding a project changed shared credentials"
        multi_workflow = (root / "WORKFLOW.md").read_bytes()
        # Recover the database-created / workflow-not-published interruption window.
        (root / "WORKFLOW.md").write_bytes(saved["WORKFLOW.md"])
        run(add, env)
        run([cli, "project", "add", "--repo", "https://github.com/example/second", "--project-id", mappings[1][1], "--identifier", "SECOND"], env)
        for options in (("--repo", "https://github.com/example/wrong", "--project-id", mappings[1][1], "--identifier", "SECOND"),
                        ("--repo", "https://github.com/example/second", "--identifier", "OTHER")):
            result = run([cli, "project", "add", *options], env, check=False)
            assert result.returncode != 0, "Conflicting project remap was accepted"
            no_secrets(result, settings)
        assert (root / "WORKFLOW.md").read_bytes() == multi_workflow
        assert database_state(second_settings) == multi_state and public_api(second_settings) == second_api
        assert get_json(plain, task_path, headers) == task_before
        login(settings)
        saved["WORKFLOW.md"] = multi_workflow
        db_state = multi_state

        print("Checking copied legacy service identity without credential replacement", flush=True)
        legacy_key = settings["api_key"][:42]
        convert = """import json,sys
from plane.db.models import User,APIToken
from django.db import transaction
data=json.load(sys.stdin)
with transaction.atomic():
    account=User.objects.get(pk=data['automation_id'])
    assert account.is_bot and not account.has_usable_password()
    token=APIToken.objects.get(user=account,token=data['current_key'])
    account.is_bot=False
    account.save(update_fields=['is_bot'])
    token.user_type=0
    token.token=data['legacy_key']
    token.save(update_fields=['user_type','token'])
"""
        run([root / "scripts/plane", "exec", "-T", "api", "python", "manage.py", "shell", "-c", convert], env,
            data=json.dumps({"automation_id": settings["automation_id"], "current_key": settings["api_key"], "legacy_key": legacy_key}).encode())
        (root / ".env").write_bytes((root / ".env").read_bytes().replace(settings["api_key"].encode(), legacy_key.encode()))
        settings["api_key"] = legacy_key
        (root / "plane.json").write_text(json.dumps(settings, indent=2) + "\n")
        legacy_identity = True
        identity_before = database_state(settings)
        legacy_private = {name: (root / name).read_bytes() for name in files[:-1]}
        legacy_add = [cli, "project", "add", "--repo", "https://github.com/example/legacy"]
        result = run(legacy_add, env)
        no_secrets(result, settings)
        assert legacy_private == {name: (root / name).read_bytes() for name in files[:-1]}, "Legacy add changed existing credentials"
        saved = {name: (root / name).read_bytes() for name in files}
        legacy_state = database_state(settings)
        for model in ("User", "Workspace", "WorkspaceMember", "APIToken", "Instance", "InstanceAdmin"):
            assert legacy_state[model] == identity_before[model], "Legacy add replaced an existing identity"
        assert len(legacy_state["Project"]) == 3
        run(legacy_add, env)
        assert saved == {name: (root / name).read_bytes() for name in files}
        assert database_state(settings) == legacy_state
        wrong_owner = {**settings, "owner_id": str(uuid.uuid4())}
        (root / "plane.json").write_text(json.dumps(wrong_owner) + "\n")
        refused = run([cli, "project", "add", "--repo", "https://github.com/example/foreign"], env, check=False)
        assert refused.returncode != 0, "Legacy compatibility accepted unrelated ownership"
        no_secrets(refused, settings)
        assert (root / "WORKFLOW.md").read_bytes() == saved["WORKFLOW.md"]
        assert database_state(settings) == legacy_state
        (root / "plane.json").write_bytes(saved["plane.json"])
        assert get_json(plain, task_path, {"X-API-Key": legacy_key}) == task_before
        api_state = public_api(settings)
        login(settings)
        db_state = legacy_state

        # Simulate lost ownership metadata in this disposable instance only.
        print("Checking installation and explicit connection to a manually managed board", flush=True)
        for name in ("plane.json", ".env", "WORKFLOW.md"):
            (root / name).unlink()
        manual_init = run(init, env)
        no_secrets(manual_init, settings)
        assert not (root / "plane.json").exists(), "Init claimed ownership of a manually managed board"
        assert not (root / ".env").exists() and not (root / "WORKFLOW.md").exists()
        assert database_state(settings) == db_state, "Init changed existing Plane records"
        assert public_api(settings) == api_state
        login(settings)
        manual_connect = run([cli, "project", "add", "--repo", "https://github.com/example/project",
                              "--workspace", settings["workspace"], "--project-id", settings["project_id"],
                              "--identifier", settings["identifier"], "--api-key-stdin"], env,
                             data=(settings["api_key"] + "\n").encode())
        no_secrets(manual_connect, settings)
        assert not (root / "plane.json").exists(), "Manual connection created managed-board ownership metadata"
        assert database_state(settings) == db_state, "Manual connection changed existing identities"
        assert public_api(settings) == api_state
        assert settings["api_key"].encode() in (root / ".env").read_bytes()
        mappings = run([cli, "project", "list"], env).stdout.decode().splitlines()[1:]
        assert [line.split() for line in mappings] == [["APP", settings["project_id"], "https://github.com/example/project"]]
        for name in ("plane.json", ".env", "WORKFLOW.md"):
            (root / name).write_bytes(saved[name])
            (root / name).chmod(0o600)
        assert not marker.exists(), "Init or project add launched an agent or changed a systemd service"
        print("Installation-only init, explicit project connections, preserved data, legacy identity reuse and ownership/remap checks passed")
    finally:
        assert tmp.name.startswith("switchyard-local-setup-") and root.is_relative_to(tmp)
        assert project == "switchyard-" + hashlib.sha256(os.fsencode(root.resolve())).hexdigest()[:12]
        if (root / "scripts/plane").exists() and (root / ".env.plane").exists():
            run([root / "scripts/plane", "down", "--volumes", "--remove-orphans"], env)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cli", type=Path, help="built CLI to test against a disposable full Plane instance")
    args = parser.parse_args()
    if args.cli is None:
        print("Disposable local setup check skipped; pass --cli to run with Docker")
    else:
        cli = args.cli.resolve()
        if not cli.is_file() or not os.access(cli, os.X_OK):
            parser.error("--cli must name an executable built CLI")
        with tempfile.TemporaryDirectory(prefix="switchyard-local-setup-") as directory:
            check(cli, Path(directory))
