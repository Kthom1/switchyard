# Source and dependency versions

Switchyard's runner is [Yardmaster](https://github.com/Kthom1/yardmaster), a
modified [OpenAI Symphony](https://github.com/openai/symphony), at commit
`0303190df33992fcb2226779c722534116d09ec8`. The source is included under
`vendor/yardmaster`, along with its Apache-2.0 LICENSE and its NOTICE, which keeps
OpenAI's notice. Yardmaster provides the Plane adapter, Agent Client Protocol
agents, the agent sandbox, turn-event correlation, the task Git directory policy and
patched Ecto, Solid and Decimal releases.

## Local integration

- `patches/project-routing.patch` passes trusted repository and task identities
  to workspace hooks, stops runs whose target changes, and checks identity before
  cleanup with Switchyard's `scripts/task-workspace`.
- `scripts/build` applies that patch and copies Switchyard's tests into the
  assembled runtime under `work/yardmaster`.
- `scripts/claude-runner install` installs `@agentclientprotocol/claude-agent-acp`
  0.84.0 under `work/acp` for the optional [Claude Code agent](agents.md).

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
| MinIO | `docker.io/pgsty/minio@sha256:b6bfe7239bfc83fb90d31612d9704d86039dd714f7904b3f1ad68f211e602372` |

`scripts/plane` applies both the base configuration and `deploy/compose.override.yml`.
The override pins MinIO by digest, binds the proxy to loopback and redacts API keys
from proxy logs. Other images use version tags. MinIO no longer publishes pullable
images, so the pin follows Plane in using the community `pgsty/minio`
RELEASE.2026-08-04T00-00-00Z build.

Run `scripts/plane config --images` to see the selected images for your installation.
Each backup also records that list. See the [backup and restore guide](backup.md)
before changing image versions.

## Source archives

Run `scripts/package` from a clean, committed checkout with the submodule initialized.
It writes a source archive and SHA-256 checksum under `work/dist/`. The archive
contains tracked files and the pinned Yardmaster source, including their licenses.
Local configuration, task workspaces, logs and Git metadata are excluded.

Extract the archive into a new directory and follow the [source-build
steps](getting-started.md). `scripts/build` uses the included Yardmaster source without requiring Git
metadata. Contributors can also use a recursive Git clone.

## Linux runner archives

`scripts/build-runner` packages the assembled runtime with Yardmaster's Burrito
release configuration and installs it as `bin/symphony`, the path the CLI and
existing installations use. The Linux x86-64 executable
includes Erlang/OTP and Elixir. `BUILD.txt` records the source revisions and
build versions; `licenses/` contains the dependency lockfile and notices. The
archive also includes the corresponding Switchyard and pinned Yardmaster source.

The runner archive is an intermediate build artifact. `scripts/build-cli` adds
the Go CLI to produce the complete [installation bundle](cli.md). Git, GitHub CLI,
Codex, credentials and your repository's build tools remain host prerequisites. Plane still runs
in Docker. Burrito extracts its runtime to a user cache on first launch; that
location must permit execution. `YARDMASTER_INSTALL_DIR` can select another path.
Runtimes extracted by runners built before Yardmaster stay under `symphony_*` in that
cache and can be removed once no older runner is in use.
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
