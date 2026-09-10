"""Run the real assembly twice; removed source must disappear and caches must survive."""
import shutil
import subprocess
import tempfile
from pathlib import Path

source = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    subprocess.run(["git", "init", "--quiet", root], check=True)
    for name in ("scripts", "integration", "test", "patches"):
        shutil.copytree(source / name, root / name)
    (root / "vendor").mkdir()
    (root / "vendor/symphony").symlink_to(source / "vendor/symphony", target_is_directory=True)
    command = ["bash", root / "scripts/build", "--assemble-only"]
    subprocess.run(command, check=True, stdout=subprocess.DEVNULL)
    runtime = root / "work/symphony/elixir"
    stale = [runtime / "lib/deleted.ex", runtime / "test/deleted_test.exs"]
    caches = [runtime / "deps/keep", runtime / "_build/keep", runtime / "bin/symphony"]
    for path in stale + caches:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("sentinel")
    subprocess.run(command, check=True, stdout=subprocess.DEVNULL)
    assert all(not path.exists() for path in stale)
    assert all(path.read_text() == "sentinel" for path in caches)
    assert (runtime / "lib/symphony_elixir/plane.ex").read_bytes() == (source / "integration/plane.ex").read_bytes()
    assert (runtime / "lib/symphony_elixir/tracker.ex").read_text().count('"plane" =>') == 1
    assert "resolve_workspace_git_policy" in (runtime / "lib/symphony_elixir/config/schema.ex").read_text()
    shutil.rmtree(root / ".git")
    subprocess.run(command, check=True, stdout=subprocess.DEVNULL)
    assert (runtime / "lib/symphony_elixir/tracker.ex").read_text().count('"plane" =>') == 1
print("Assembly deletion, cache/executable preservation, repeatability, and source-archive checks passed")
