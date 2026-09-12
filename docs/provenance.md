# Source and dependency versions

Switchyard uses [OpenAI Symphony](https://github.com/openai/symphony) at commit
`8001b52e3062495a16e520e4ceaf8f9de868c4d0`. The source is included under
`vendor/symphony`, along with its Apache-2.0 LICENSE and NOTICE.

## Local integration

- `patches/workspace-git.patch` resolves the task-specific writable Git directory.
- `patches/project-routing.patch` passes trusted repository and task identities
  to workspace hooks, stops runs whose target changes, and checks identity before cleanup.
- `patches/dependencies.patch` pins Ecto 3.13.6, Solid 1.3.3 and Decimal 3.1.1.
- `scripts/build` registers the Plane adapter and copies its implementation and tests
  into the assembled runtime under `work/symphony`.

Decimal 3.1.1 includes the fix described in the maintainer's
[GHSA-rhv4-8758-jx7v advisory](https://github.com/ericmj/decimal/security/advisories/GHSA-rhv4-8758-jx7v).

## Container images

The base Compose file comes from Plane's v1.4.2 release.

| Component | Image |
| --- | --- |
| Plane frontend, space, admin, live, backend and proxy | `makeplane/plane-*:v1.4.2` |
| Postgres | `postgres:15.7-alpine` |
| Valkey | `valkey/valkey:7.2.11-alpine` |
| RabbitMQ | `rabbitmq:3.13.6-management-alpine` |
| MinIO | `quay.io/minio/minio@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e` |

`scripts/plane` applies both the base configuration and `deploy/compose.override.yml`.
The override pins MinIO by digest, binds the proxy to loopback and redacts API keys
from proxy logs. Other images use version tags.

Run `scripts/plane config --images` to see the selected images for your installation.
Each backup also records that list. See the [backup and restore guide](backup.md)
before changing image versions.

## Source archives

Run `scripts/package` from a clean, committed checkout with the submodule initialized.
It writes a source archive and SHA-256 checksum under `work/dist/`. The archive
contains tracked files and the pinned Symphony source, including their licenses.
Local configuration, task workspaces, logs and Git metadata are excluded.

Extract the archive into a new directory and follow the [source-build
steps](getting-started.md). `scripts/build` uses the included Symphony source without requiring Git
metadata. Contributors can also use a recursive Git clone.

## Linux runner archives

`scripts/build-runner` packages the assembled Switchyard adapter and patches
with the upstream Burrito release configuration. The Linux x86-64 executable
includes Erlang/OTP and Elixir. `BUILD.txt` records the source revisions and
build versions; `licenses/` contains the dependency lockfile and notices. The
archive also includes the corresponding Switchyard and pinned Symphony source.

The runner archive is an intermediate build artifact. `scripts/build-cli` adds
the Go CLI to produce the complete [installation bundle](cli.md). Git, GitHub CLI,
Codex, credentials and your repository's build tools remain host prerequisites. Plane still runs
in Docker. Burrito extracts its runtime to a user cache on first launch; that
location must permit execution. `SYMPHONY_INSTALL_DIR` can select another path.
Each native build uses release version `0.1.0+FULL_GIT_COMMIT`, recorded in
`BUILD.txt`, so Burrito selects the runtime belonging to that source revision.
To verify an upgrade with a previous runtime already cached, run:

```bash
python3 test/multi_project_test.py --runner /path/to/new/bin/symphony \
  --previous-runner /path/to/previous/bin/symphony
```

The check uses a disposable user home, two local Git repositories, and simulated
Plane and Codex app-server endpoints.

For installation, use the complete CLI bundle and its [PATH setup](cli.md#install-on-your-path).
Keep the configured installation and its Plane volumes separate from release files.
