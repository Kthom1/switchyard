"""Check the Claude runner and agent sandbox without starting a model or reading account secrets."""
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile

source = Path(__file__).resolve().parent.parent
(source / "work").mkdir(exist_ok=True)
umask = os.umask(0)
os.umask(umask)
sandbox_usable = shutil.which("bwrap") and subprocess.run(
    ["bwrap", "--ro-bind", "/", "/", "true"], capture_output=True
).returncode == 0

with tempfile.TemporaryDirectory() as temporary, tempfile.TemporaryDirectory(dir=source / "work") as protected:
    root = Path(temporary)
    (root / "scripts").mkdir()
    (root / "bin").mkdir()
    for script in ("claude-runner", "agent-sandbox"):
        shutil.copy2(source / "scripts" / script, root / "scripts" / script)
    env = {**os.environ, "PATH": str(root / "bin") + ":" + os.environ["PATH"]}
    env.pop("SWITCHYARD_CLAUDE_HOME", None)
    runner = str(root / "scripts/claude-runner")

    missing = subprocess.run([runner, "acp"], text=True, env=env, capture_output=True)
    assert missing.returncode != 0 and "claude-runner install" in missing.stderr
    home = root / "work/claude"
    assert stat.S_IMODE(home.stat().st_mode) == 0o700

    npm = root / "bin/npm"
    npm.write_text('#!/usr/bin/env python3\nimport json,sys\nprint(json.dumps(sys.argv[1:]))\n')
    npm.chmod(0o700)
    install = json.loads(subprocess.check_output([runner, "install"], text=True, env=env))
    assert install[-1].startswith("@agentclientprotocol/claude-agent-acp@") and "--save-exact" in install

    modules = root / "work/acp/node_modules"
    (modules / ".bin").mkdir(parents=True)
    (modules / "@anthropic-ai/claude-agent-sdk-linux-x64").mkdir(parents=True)
    probe = (
        "#!/usr/bin/env python3\nimport json,os,sys\n"
        "def write(path):\n"
        "    try:\n        open(path, 'w').write('x'); return True\n"
        "    except OSError:\n        return False\n"
        "print(json.dumps({'home': os.environ['CLAUDE_CONFIG_DIR'], 'args': sys.argv[1:],\n"
        "    'checkout': write('inside.txt'), 'claude_home': write(os.environ['CLAUDE_CONFIG_DIR'] + '/state'),\n"
        f"    'outside': write({str(Path(protected) / 'escape.txt')!r}), 'mode': oct(os.stat('inside.txt').st_mode & 0o777) if os.path.exists('inside.txt') else None}}))\n"
    )
    for fake in (modules / ".bin/claude-agent-acp", modules / "@anthropic-ai/claude-agent-sdk-linux-x64/claude"):
        fake.write_text(probe)
        fake.chmod(0o700)

    checkout = root / "task"
    checkout.mkdir()
    interactive = json.loads(subprocess.check_output([runner, "--version"], text=True, env=env, cwd=checkout))
    assert interactive["home"] == str(home) and interactive["args"] == ["--version"]
    for written in (checkout / "inside.txt", home / "state", Path(protected) / "escape.txt"):
        written.unlink()

    if sandbox_usable:
        acp = json.loads(subprocess.check_output([runner, "acp"], text=True, env=env, cwd=checkout))
        assert acp == {"home": str(home), "args": [], "checkout": True, "claude_home": True, "outside": False, "mode": oct(0o666 & ~umask)}, acp
        assert not (Path(protected) / "escape.txt").exists()
        sandbox = str(root / "scripts/agent-sandbox")
        for bad, cwd in (([sandbox, "--writable", "relative", "--", "true"], checkout), ([sandbox, "--", "true"], Path.home())):
            refused = subprocess.run(bad, text=True, env=env, cwd=cwd, capture_output=True)
            assert refused.returncode != 0, bad
        print("Claude runner uses its private home; the sandbox allows only the checkout, /tmp and that home.")
    else:
        print("Claude runner uses its private home; skipped sandbox checks because bubblewrap is unavailable.")

    env["SWITCHYARD_CLAUDE_HOME"] = "relative"
    bad = subprocess.run([runner, "acp"], text=True, env=env, capture_output=True)
    assert bad.returncode != 0 and "absolute" in bad.stderr
