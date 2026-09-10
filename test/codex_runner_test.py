"""Check runner launch scope without starting a model or reading account secrets."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

source = Path(__file__).resolve().parent.parent
with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary)
    (root / "scripts").mkdir()
    (root / "bin").mkdir()
    shutil.copy2(source / "scripts/codex-runner", root / "scripts/codex-runner")
    fake = root / "bin/codex"
    fake.write_text('#!/usr/bin/env python3\nimport os,sys,json\nassert "SOURCE_REPO_URL" not in os.environ\nprint(json.dumps({"home":os.environ.get("CODEX_HOME"),"args":sys.argv[1:]}))\n')
    fake.chmod(0o700)
    env = {**os.environ, "PATH": str(root / "bin") + ":" + os.environ["PATH"]}
    env.pop("SWITCHYARD_CODEX_HOME", None)
    env.pop("CODEX_HOME", None)
    env["SOURCE_REPO_URL"] = "https://example.invalid/stale-repository.git"
    command = [str(root / "scripts/codex-runner"), "app-server"]
    result = json.loads(subprocess.check_output(command, text=True, env=env))
    assert result == {"home": None, "args": ["app-server"]}
    assert not (root / "work/codex").exists()
    env["CODEX_HOME"] = str(root / "custom home")
    env["SWITCHYARD_CODEX_HOME"] = str(root / "legacy")
    result = json.loads(subprocess.check_output(command, text=True, env=env))
    assert result == {"home": env["CODEX_HOME"], "args": ["app-server"]}
    env.pop("CODEX_HOME")
    result = json.loads(subprocess.check_output(command, text=True, env=env))
    assert result == {"home": env["SWITCHYARD_CODEX_HOME"], "args": ["app-server"]}
    env["SWITCHYARD_CODEX_HOME"] = "relative"
    bad = subprocess.run(command, text=True, env=env, capture_output=True)
    assert bad.returncode != 0 and "absolute" in bad.stderr
print("Runner preserves native Codex defaults and explicit configuration, with legacy home compatibility.")
