"""The bundle launcher preserves environment, credentials and workspace checks."""
import json
from pathlib import Path
import shutil
import subprocess
import tempfile

source = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary) / "installation with spaces"
    (root / "scripts").mkdir(parents=True)
    (root / "bin").mkdir()
    for name in ("run", "task-workspace"):
        shutil.copy2(source / "scripts" / name, root / "scripts" / name)
    (root / "WORKFLOW.md").write_text("test workflow")
    env = root / ".env"
    task_root = Path(temporary) / "tasks"
    env.write_text(f"SYMPHONY_WORKSPACE_ROOT='{task_root}'\nPLANE_API_KEY=host-only\n")
    binary = root / "bin/symphony"
    binary.write_text('#!/usr/bin/env python3\nimport json,os,sys\nprint(json.dumps({"root":os.environ["SWITCHYARD_ROOT"],"key":os.environ["PLANE_API_KEY"],"args":sys.argv[1:]}))\n')
    binary.chmod(0o700)
    result = json.loads(subprocess.check_output([root / "scripts/run"], text=True))
    assert result["root"] == str(root)
    assert result["key"] == "host-only"
    assert result["args"] == [str(root / "WORKFLOW.md"), "--logs-root", str(root / "work/log"),
                              "--i-understand-that-this-will-be-running-without-the-usual-guardrails"]
    assert task_root.is_dir()
    subprocess.run(["git", "init", "-q", root], check=True)
    env.write_text(f"SYMPHONY_WORKSPACE_ROOT='{root / 'nested'}'\n")
    rejected = subprocess.run([root / "scripts/run"], capture_output=True, text=True)
    assert rejected.returncode != 0 and "outside every source checkout" in rejected.stderr
print("Packaged launcher preserves arguments, host credentials and isolated task-root validation.")

with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary)
    (root / "scripts").mkdir()
    shutil.copy2(source / "scripts/build-runner", root / "scripts/build-runner")
    subprocess.run(["git", "init", "-q", root], check=True)
    subprocess.run(["git", "-C", root, "add", "."], check=True)
    subprocess.run(["git", "-C", root, "-c", "user.name=Test",
                    "-c", "user.email=test@example.invalid", "commit", "-qm", "initial"], check=True)
    (root / "patches").mkdir()
    (root / "patches/untracked.patch").write_text("untracked build input")
    rejected = subprocess.run([root / "scripts/build-runner"], capture_output=True, text=True)
    assert rejected.returncode != 0 and "Commit all source changes" in rejected.stderr
    assert not (root / "work").exists()
print("Runner packaging rejects untracked patches before assembling source.")
