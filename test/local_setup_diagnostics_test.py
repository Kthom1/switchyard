"""Keep fresh-setup failure diagnostics useful without exposing captured secrets."""
from pathlib import Path
import subprocess
import tempfile

from local_setup_test import failure_summary, run


with tempfile.TemporaryDirectory(prefix="switchyard-diagnostic-") as directory:
    root = Path(directory)
    env = {"SWITCHYARD_HOME": directory}
    secret = b"synthetic-private-password-and-token"
    failure = subprocess.CompletedProcess([], 1, secret, b"toomanyrequests: " + secret)
    assert failure_summary(failure, env) == "category=registry rate limit; prepared=none"
    (root / "bin").mkdir()
    (root / "bin/symphony").write_bytes(secret)
    (root / ".env.plane").write_bytes(secret)
    failure.stderr = b"unrecognized provider message: " + secret
    assert failure_summary(failure, env) == "category=unclassified; prepared=bin/symphony,.env.plane"
    failure.stderr = b"Cannot connect to the Docker daemon: " + secret
    assert failure_summary(failure, env) == "category=Docker daemon unavailable; prepared=bin/symphony,.env.plane"
    try:
        run(["/bin/sh", "-c", "printf synthetic-private-password-and-token >&2; exit 7"], env)
    except RuntimeError as error:
        assert "exit 7" in str(error) and "category=unclassified" in str(error)
        assert secret.decode() not in str(error) and directory not in str(error)
    else:
        raise AssertionError("Failed command was accepted")
print("Local setup diagnostics retain failure status and withhold private output")
