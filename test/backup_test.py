"""Failure checks by default; --docker proves recovery; --cli tests native commands; --restic adds encryption."""
import argparse
import hashlib
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
ENV = {k: v for k, v in os.environ.items() if not k.startswith(("SWITCHYARD_", "RESTIC_", "AWS_"))}


def run(args, *, env=None, data=None, check=True):
    result = subprocess.run([str(arg) for arg in args], input=data, stdin=subprocess.DEVNULL if data is None else None, capture_output=True, env=env or ENV)
    if check and result.returncode:
        raise RuntimeError(f"Command failed ({result.returncode}): {args[0]}\n{result.stderr.decode(errors='replace')}")
    return result


def install(path, configured=True, cli=False):
    (path / "scripts").mkdir(parents=True)
    for name in ("plane", "backup", "restore"):
        shutil.copy2(ROOT / "scripts" / name, path / "scripts" / name)
    shutil.copytree(ROOT / "deploy", path / "deploy")
    if configured:
        shutil.copyfile(ROOT / "test/fixtures/plane.env", path / ".env.plane")
        (path / ".env.plane").chmod(0o600)
        (path / ".env").write_text("PLANE_API_KEY=synthetic-backup-check\n")
        (path / "WORKFLOW.md").write_text("Synthetic workflow, never dispatched.\n")
        (path / "work/codex").mkdir(parents=True)
        (path / "work/codex/auth.json").write_text('{"synthetic": true}\n')
        (path / "work/codex/config.toml").write_text('# Synthetic runner configuration\n')
        if cli:
            (path / "config.json").write_text('{"port":8090,"runner_port":8091,"web_url":"http://localhost:8090"}\n')
            (path / "plane.json").write_text('{"owner_password":"synthetic-owner-password","api_key":"synthetic-backup-check"}\n')
    return path


def failure_check(tmp):
    source = install(tmp / "failure-source")
    binaries = tmp / "bin"
    binaries.mkdir()
    docker = binaries / "docker"
    docker.write_text("""#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
if args[0] == 'volume':
    sys.exit(0)
if 'ps' in args:
    print(os.environ.get('SWITCHYARD_TEST_RUNNING', 'plane-db'))
elif '--images' in args:
    print('postgres:synthetic-check')
elif 'config' in args:
    print(json.dumps({'services': {'plane-db': {'image': 'postgres:synthetic-check'}}}))
elif 'exec' in args and 'pg_isready' in args[-1]:
    sys.exit(0)
elif 'exec' in args:
    sys.stdout.write('partial dump')
    sys.exit(9)
else:
    sys.exit(95)
""")
    docker.chmod(0o700)
    env = {**ENV, "PATH": str(binaries) + os.pathsep + ENV["PATH"]}
    output = tmp / "backups"
    output.mkdir()
    previous = output / "previous-valid-backup"
    previous.write_bytes(b"keep this preceding backup")
    result = run([source / "scripts/backup", output], env=env, check=False)
    assert result.returncode == 9, result.stderr
    assert previous.read_bytes() == b"keep this preceding backup"
    assert list(output.iterdir()) == [previous], "Failed backup left a published or incomplete directory"
    result = run([source / "scripts/backup", output], env={**env, "SWITCHYARD_TEST_RUNNING": "plane-db\napi"}, check=False)
    assert result.returncode != 0 and b"Stop Plane" in result.stderr
    assert previous.read_bytes() == b"keep this preceding backup"
    print("Backup dump-failure preservation and running-writer refusal passed")

    # Model the image entrypoint: its temporary UNIX server is ready before TCP.
    backup = output / "synthetic-complete"
    (backup / "config").mkdir(parents=True)
    for name in (".env", ".env.plane", "WORKFLOW.md"):
        shutil.copy2(source / name, backup / "config" / name)
    for name in ("database.dump", "uploads.tar"):
        (backup / name).write_bytes(b"synthetic readiness fixture")
    files = [p for p in backup.rglob("*") if p.is_file()]
    (backup / "SHA256SUMS").write_text("".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.relative_to(backup)}\n" for p in files))
    state = tmp / "readiness-state"
    docker.write_text("""#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
state = pathlib.Path(os.environ['SWITCHYARD_TEST_READINESS'])
if 'config' in args:
    print(json.dumps({'services': {'plane-db': {'image': 'postgres:synthetic-check'}}}))
elif args[0] == 'compose' and 'ps' in args:
    print('plane-db')
elif 'exec' in args and 'pg_isready' in args[-1]:
    if '-h 127.0.0.1' in args[-1]:
        attempts = int(state.read_text()) + 1 if state.exists() else 1
        state.write_text(str(attempts))
        sys.exit(0 if attempts >= 2 else 1)
    # The temporary socket server already accepts readiness checks.
    sys.exit(0)
elif 'exec' in args and ('pg_restore' in args[-1] or 'pg_dump' in args[-1]):
    if not state.exists() or int(state.read_text()) < 2:
        sys.exit(42)
    if 'pg_dump' in args[-1]:
        sys.stdout.write('synthetic dump')
    else:
        with state.with_suffix('.writes').open('a') as writes: writes.write('restored once;')
        sys.stdin.buffer.read()
elif args[0] == 'run':
    sys.stdin.buffer.read()
""")
    target = install(tmp / "readiness-target", configured=False)
    readiness_env = {**env, "SWITCHYARD_COMPOSE_PROJECT": "switchyard-readiness-check", "SWITCHYARD_TEST_READINESS": str(state), "SWITCHYARD_VERSION": "synthetic-release"}
    result = run([source / "scripts/backup", output], env=readiness_env, check=False)
    assert result.returncode == 0, result.stderr
    assert state.read_text() == "2", "Backup did not wait for final TCP readiness"
    completed = next(output.glob("switchyard-*"))
    assert (completed / "source.txt").read_text() == "synthetic-release\n"
    assert not (completed / "config/config.json").exists(), "Legacy backups unexpectedly require CLI settings"
    state.unlink()
    result = run([target / "scripts/restore", backup], env=readiness_env, check=False)
    assert result.returncode == 0, result.stderr
    assert state.read_text() == "2", "Restore did not wait for final TCP readiness"
    assert state.with_suffix(".writes").read_text() == "restored once;"
    assert not (target / "config.json").exists()
    print("Legacy backup and restore wait for final TCP readiness without requiring CLI settings")


def docker_check(tmp, use_restic, cli=None):
    source = install(tmp / "docker-source", cli=True)
    target = tmp / "docker-restore's copy" if cli else install(tmp / "docker-restore", configured=False)
    nonce = uuid.uuid4().hex[:12]
    projects = [f"switchyard-backup-check-{nonce}", f"switchyard-restore-check-{nonce}"]
    if cli:
        # Match the CLI contract using these unique, disposable absolute roots only.
        projects = ["switchyard-" + hashlib.sha256(os.fsencode(path.resolve())).hexdigest()[:12] for path in (source, target)]
    source_env = {**ENV, "SWITCHYARD_COMPOSE_PROJECT": projects[0]}
    target_env = {**ENV, "SWITCHYARD_COMPOSE_PROJECT": projects[1]}
    backup_command = [source / "scripts/backup"]
    restore_command = [target / "scripts/restore"]
    if cli:
        native_home = tmp / "native-home"
        native_home.mkdir()
        source_env.update(HOME=str(native_home), SWITCHYARD_HOME=str(source),
                          SWITCHYARD_RUNNER_ENV=str(tmp / "unrelated.env"), SWITCHYARD_WORKFLOW=str(tmp / "unrelated-workflow"))
        target_env.update(HOME=str(native_home), SWITCHYARD_HOME=str(target))
        (source / ".env").write_text("PLANE_API_KEY='synthetic-backup-check'\nSOURCE_REPO_URL='https://github.com/example/synthetic.git'\n" +
                                     "SYMPHONY_WORKSPACE_ROOT=" + shlex.quote(str(source / "workspaces")) + "\n")
        bundle = tmp / "synthetic runner bundle"
        (bundle / "bin").mkdir(parents=True)
        (bundle / "licenses").mkdir()
        (bundle / "licenses/NOTICE").write_text("Synthetic runner for recovery checks only.\n")
        runner = bundle / "bin/symphony"
        runner_marker = tmp / "runner-launched"
        runner.write_text("#!/usr/bin/env sh\nprintf launched > " + shlex.quote(str(runner_marker)) + "\nexit 99\n")
        runner.chmod(0o700)
        unit = native_home / ".config/systemd/user" / (projects[1] + ".service")
        backup_command = [cli, "backup"]
        restore_command = [cli, "restore", "--runner", runner]
    image = "postgres:15.7-alpine"
    attachment = b"Synthetic attachment for Switchyard restore\x00\xff\n"
    try:
        run([source / "scripts/plane", "up", "-d", "plane-db"], env=source_env)
        for _ in range(60):
            ready = run([source / "scripts/plane", "exec", "-T", "plane-db", "pg_isready", "-h", "127.0.0.1", "-U", "plane", "-d", "plane"], env=source_env, check=False)
            if ready.returncode == 0:
                break
            time.sleep(1)
        assert ready.returncode == 0, "Disposable Postgres did not become ready"
        sql = b"""CREATE TABLE backup_probe_tasks (id text PRIMARY KEY, name text NOT NULL);
CREATE TABLE backup_probe_comments (task_id text REFERENCES backup_probe_tasks(id), body text NOT NULL);
INSERT INTO backup_probe_tasks VALUES ('task-1', 'Synthetic backup task');
INSERT INTO backup_probe_comments VALUES ('task-1', 'Synthetic backup comment');
"""
        run([source / "scripts/plane", "exec", "-T", "plane-db", "psql", "-h", "/var/run/postgresql", "-v", "ON_ERROR_STOP=1", "-U", "plane", "-d", "plane"], env=source_env, data=sql)
        run(["docker", "volume", "create", projects[0] + "_uploads"])
        run(["docker", "run", "--rm", "-i", "--pull=never", "--network", "none", "--read-only", "--mount", f"type=volume,source={projects[0]}_uploads,target=/data", "--entrypoint", "sh", image, "-c", "cat > /data/attachment.bin"], data=attachment)
        run(backup_command, env=source_env)
        backups = source / "work/backups"
        backup = next(backups.glob("switchyard-*"))
        for path in backup.rglob("*"):
            assert path.stat().st_mode & 0o777 == (0o700 if path.is_dir() else 0o600), path
        if cli:
            assert (backup / "source.txt").read_bytes() == run([cli, "version"]).stdout
            assert run([source / "scripts/plane", "ps", "--status", "running", "--services"], env=source_env).stdout.strip() == b"plane-db"
            unit.parent.mkdir(parents=True)
            unit.write_text("Synthetic stale service unit.\n")
            refused = run([*restore_command, backup], env=target_env, check=False)
            assert refused.returncode != 0 and b"service" in refused.stderr.lower(), refused.stderr
            assert not target.exists() or not list(target.iterdir()), "Stale service refusal modified the restore destination"
            unit.unlink()
        run([*restore_command, backup], env=target_env)
        query = "SELECT t.name, c.body FROM backup_probe_tasks t JOIN backup_probe_comments c ON c.task_id = t.id;"
        result = run([target / "scripts/plane", "exec", "-T", "plane-db", "psql", "-h", "/var/run/postgresql", "-At", "-U", "plane", "-d", "plane", "-c", query], env=target_env)
        assert result.stdout.strip() == b"Synthetic backup task|Synthetic backup comment"
        restored = run(["docker", "run", "--rm", "--pull=never", "--network", "none", "--read-only", "--mount", f"type=volume,source={projects[1]}_uploads,target=/data,readonly", "--entrypoint", "cat", image, "/data/attachment.bin"])
        assert hashlib.sha256(restored.stdout).digest() == hashlib.sha256(attachment).digest()
        assert "config/config.json" in (backup / "SHA256SUMS").read_text()
        assert "config/plane.json" in (backup / "SHA256SUMS").read_text()
        for name in (".env", ".env.plane", "WORKFLOW.md", "config.json", "plane.json", "work/codex/auth.json", "work/codex/config.toml"):
            if cli and name == ".env":
                assert (target / name).read_bytes().startswith((source / name).read_bytes())
            else:
                assert (source / name).read_bytes() == (target / name).read_bytes()
            assert (target / name).stat().st_mode & 0o777 == 0o600
        assert run([*restore_command, backup], env=target_env, check=False).returncode != 0
        if cli:
            workspace = run(["bash", "-c", 'source "$1"; printf %s "$SYMPHONY_WORKSPACE_ROOT"', "bash", target / ".env"], env=target_env)
            assert workspace.stdout.decode() == str(target / "workspaces")
            assert not unit.exists(), "Restore created a runner service"
            assert not runner_marker.exists(), "Recovery launched an agent"
            assert run([target / "scripts/plane", "ps", "--status", "running", "--services"], env=target_env).stdout.strip() == b"plane-db"
            print("Native backup/restore preserves data, relocates quoted workspace paths, rejects stale units, and leaves only Postgres running")
        fresh = install(tmp / "existing-volume-check", configured=False)
        refused = run([fresh / "scripts/restore", backup], env=target_env, check=False)
        assert refused.returncode != 0 and b"existing" in refused.stderr
        assert not (fresh / ".env.plane").exists()
        unused_env = {**ENV, "SWITCHYARD_COMPOSE_PROJECT": projects[1] + "-unused"}
        for name in ("config.json", "plane.json"):
            existing_config = install(tmp / ("existing-" + name), configured=False)
            (existing_config / name).write_bytes(b"retain this configuration")
            refused = run([existing_config / "scripts/restore", backup], env=unused_env, check=False)
            assert refused.returncode != 0 and ("existing configuration: " + name).encode() in refused.stderr
            assert (existing_config / name).read_bytes() == b"retain this configuration"
            assert not (existing_config / ".env.plane").exists()
        for name in ("database.dump", "config/config.json", "config/plane.json", "config/WORKFLOW.md"):
            damaged = tmp / "damaged-backup"
            shutil.copytree(backup, damaged)
            if name == "config/WORKFLOW.md":
                (damaged / name).unlink()
            else:
                (damaged / name).write_bytes(b"corrupt")
            rejected = run([fresh / "scripts/restore", damaged], env=unused_env, check=False)
            assert rejected.returncode != 0, rejected.stderr
            assert b"checksum" in rejected.stderr.lower() or b"could not be read" in rejected.stderr.lower(), rejected.stderr
            assert not (fresh / ".env.plane").exists()
            assert not (fresh / "config.json").exists()
            shutil.rmtree(damaged)
        print("Isolated DB/upload and CLI configuration restore, overwrite refusal, and incomplete/corrupt backup checks passed")
        if use_restic:
            password = tmp / "restic-password"
            password.write_text(uuid.uuid4().hex)
            password.chmod(0o600)
            restic_env = {**source_env, "RESTIC_REPOSITORY": str(tmp / "restic-repository"), "RESTIC_PASSWORD_FILE": str(password)}
            run(["restic", "init"], env=restic_env)
            before = set(backups.glob("switchyard-*"))
            run(backup_command, env=restic_env)
            uploaded = (set(backups.glob("switchyard-*")) - before).pop()
            run(["restic", "check", "--read-data"], env=restic_env)
            retrieved = tmp / "restic-retrieved"
            run(["restic", "restore", "latest", "--target", retrieved], env=restic_env)
            recovered_backup = next(retrieved.rglob("SHA256SUMS")).parent
            assert (recovered_backup / "database.dump").read_bytes() == (uploaded / "database.dump").read_bytes()
            assert (recovered_backup / "uploads.tar").read_bytes() == (uploaded / "uploads.tar").read_bytes()
            assert (recovered_backup / "config/codex/auth.json").read_bytes() == (uploaded / "config/codex/auth.json").read_bytes()
            before = set(backups.glob("switchyard-*"))
            failed = run(backup_command, env={**restic_env, "RESTIC_REPOSITORY": str(tmp / "missing-restic-repository")}, check=False)
            assert failed.returncode != 0 and b"Restic failed" in failed.stderr
            retained = (set(backups.glob("switchyard-*")) - before).pop()
            assert (retained / "database.dump").stat().st_size > 0 and (retained / "SHA256SUMS").exists()
            print("Local encrypted restic backup/check/restore and restic-failure local retention passed")
    finally:
        for project, path in zip(projects, (source, target)):
            if cli:
                assert path.is_relative_to(tmp) and tmp.name.startswith("switchyard-backup-check-")
                assert project == "switchyard-" + hashlib.sha256(os.fsencode(path.resolve())).hexdigest()[:12]
            else:
                assert project.startswith(("switchyard-backup-check-", "switchyard-restore-check-"))
            run([source / "scripts/plane", "down", "--volumes", "--remove-orphans"], env={**ENV, "SWITCHYARD_COMPOSE_PROJECT": project}, check=False)
            run(["docker", "volume", "rm", project + "_pgdata", project + "_uploads"], check=False)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--docker", action="store_true")
    parser.add_argument("--restic", action="store_true")
    parser.add_argument("--cli", type=Path, help="test a built CLI against disposable Docker projects")
    args = parser.parse_args()
    if args.restic and (not args.docker or not shutil.which("restic")):
        parser.error("--restic requires --docker and an installed restic binary")
    if args.cli:
        args.cli = args.cli.resolve()
        if not args.docker or not args.cli.is_file() or not os.access(args.cli, os.X_OK):
            parser.error("--cli requires --docker and an executable CLI path")
    with tempfile.TemporaryDirectory(prefix="switchyard-backup-check-") as directory:
        temp = Path(directory)
        failure_check(temp)
        if args.docker:
            docker_check(temp, args.restic, args.cli)
