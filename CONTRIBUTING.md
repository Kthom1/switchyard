# Contributing

[← Switchyard](README.md)

## Build the integration

Follow [Getting started](docs/getting-started.md) to install the host tools,
including the pinned Elixir/Erlang toolchain. You can build and run the checks
without configuring Plane or signing in to Codex.

`vendor/symphony` is pinned upstream source. `scripts/build` assembles generated
source in ignored `work/symphony`, removes stale source while preserving build
caches, applies explicit patches, and registers/copies the Plane adapter and tests.
The vendor checkout stays unchanged. Patches fail when their expected source moves.

Change the adapter in `integration/`, tests in `test/` and patches in `patches/`.
Do not edit generated files in `work/symphony`; the next build replaces them.
Read the closest upstream implementation before changing an integration.

In a fresh checkout, run `scripts/build` before `scripts/check` to initialize
the submodule and install locked dependencies:

```bash
scripts/build
scripts/check
```

## Build inside a workspace sandbox

Complete a host build before dispatching tasks so Erlang, Elixir, Hex and Rebar
are installed. The build reuses the installed Hex/Rebar bootstraps. Inside the
sandbox, keep writable build state and caches under the checkout. From its root:

```bash
export MISE_STATE_DIR="$PWD/work/mise-state"
export MISE_CACHE_DIR="$PWD/work/mise-cache"
export HEX_HOME="$PWD/work/hex"
scripts/build
scripts/check
```

These variables apply to this shell's build commands. They leave the host's
installed Erlang/Elixir toolchain and runner configuration in place.

## Full checks

From the repository root, after building:

```bash
scripts/check
bash test/log_redaction.sh
cd work/symphony/elixir
mise exec -- mix format --check-formatted
mise exec -- mix lint
unshare --user --map-root-user --net sh -c \
  'ip link set lo up; COLUMNS=160 mise exec -- mix test --cover --exclude live_e2e' </dev/null
mise exec -- mix dialyzer --format short
```

To exercise dispatch into two repositories, build the assembled executable and
run the acceptance check from the repository root:

```bash
(cd work/symphony/elixir && mise exec -- mix escript.build)
python3 test/multi_project_test.py --runner work/symphony/elixir/bin/symphony
```

This uses the real scheduler, workspace hooks and Git operations with temporary
repositories. A local HTTP Plane fixture and deterministic Codex app-server stub
keep it independent of accounts, paid agent runs and production data. It also
accepts the packaged native executable with `--runner`.

The test command requires `unshare`, `ip` and permission to create a user/network
namespace. The namespace keeps timing tests offline while retaining loopback.
Use detached stdin as shown; upstream dashboard assertions depend on terminal
width. Optional upstream live tests require explicit configuration and accounts.
The coverage gate excludes substantial upstream runtime modules; it is not
whole-app coverage. CI runs these checks without production credentials.

See [source and dependency versions](docs/provenance.md) for the upstream pin,
local patches and source archive packaging.

## Package the Linux runner

On Linux x86-64, commit your changes and run:

```bash
scripts/build-runner
bash test/runner_smoke.sh work/symphony/elixir/burrito_out/symphony_linux_x86_64
```

The build uses the same assembled adapter and patches, the locked production
dependencies and upstream Burrito 1.5.0 with Zig 0.15.2. Install xz and curl on
the build host. The archive and SHA-256 checksum land in `work/dist/`. It includes
`bin/symphony`, setup scripts, corresponding source and dependency notices.
The smoke check uses Docker to start the executable, query its dashboard and
stop it in Ubuntu without mise, Elixir or Erlang installed.

The `runner-package` workflow runs the same build and smoke check and uploads
workflow artifacts. It does not create tags or publish releases.

## Build the Go CLI

The CLI uses Go's standard library and the vendored go-yaml v3 parser to edit
workflow front matter. Install Go 1.25 or later; CI uses 1.27.1.

| Location | Responsibility |
| --- | --- |
| `main.go` | Executable entry point and build version. |
| `assets.go` | Embed the existing setup scripts and deployment assets. |
| `cmd/` | CLI arguments and output. |
| `config/` | Settings and installation paths. |
| `core/` | Setup, runner and service operations. |

From the repository root:

```bash
go test ./...
go vet ./...
go build -o work/switchyard .
```

To try it with an existing extracted runner bundle:

```bash
SWITCHYARD_HOME="$HOME/.local/share/switchyard-dev" \
  work/switchyard init --runner /absolute/path/to/bundle/switchyard/bin/symphony
```

For a complete Linux release bundle, commit your changes and run
`scripts/build-cli`. It builds the runner if needed, or verifies and reuses the
runner archive from the same commit. It adds the native Go executable, its
license and build version, then writes `work/dist/switchyard-REVISION-linux_x86_64.tar.gz`
and its SHA-256 checksum. The `runner-package` workflow tests Go, builds both
archives once and checks the extracted CLI and runner.
