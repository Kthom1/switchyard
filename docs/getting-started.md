# Building Switchyard from source

[← Switchyard](../README.md) · [Install the CLI](cli.md) · [Contributing](../CONTRIBUTING.md)

To install Switchyard, use the [packaged CLI](cli.md). Building from source is
for contributors who want to change Switchyard or its Symphony integration.

Use Linux x86-64 with Go 1.25+, Bash, Python 3.10+, Git, rsync, patch, curl, xz
and [mise](https://mise.jdx.dev/getting-started.html). Building from source needs host Python;
installing the packaged CLI does not. On Ubuntu, the native toolchain also needs
`build-essential pkg-config libssl-dev libncurses-dev unzip`.

```bash
git clone --recurse-submodules https://github.com/Kthom1/switchyard.git
cd switchyard
cd vendor/symphony/elixir
mise trust
mise install
cd ../../..
scripts/build
scripts/check
go test ./...
go vet ./...
```

From a clean, committed checkout, build the complete release bundle:

```bash
scripts/build-cli
```

The archive and checksum are under `work/dist/`. Follow the same
[CLI installation steps](cli.md#install-on-your-path) to put your build on PATH
and run `switchyard init`. The bundle contains the CLI, the native runner and
its runtime, licenses, and corresponding source.

See [Contributing](../CONTRIBUTING.md) for focused checks and development builds.
