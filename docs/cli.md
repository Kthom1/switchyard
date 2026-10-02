# Get started with the CLI

[← Switchyard](../README.md) · [Build from source](../CONTRIBUTING.md)

Run Switchyard on a private **Linux x86-64** machine with:

- Docker Engine and Docker Compose 2.24.4 or later, accessible to your user.
- Git, GitHub CLI (`gh`) and Bash.
- Codex CLI, unless you run [another coding agent](agents.md) such as Claude Code.
- A systemd user session.
- The build and test tools needed by your repositories.

Set up Codex on this machine with the configuration, skills and tools you want
it to use. Switchyard runs that installed Codex with your existing login and
configuration, including a custom `CODEX_HOME` if you use one.

Check your existing logins with `gh auth status` and `codex login status`.
If needed, sign in with `gh auth login` and `codex login --device-auth`.
Configure Git's `user.name` and `user.email` for commits. No separate GitHub
account or Switchyard-specific Codex login is required.
The release bundle includes the Yardmaster runner (installed as `bin/symphony`), Erlang/OTP and Elixir.

## Install on your PATH

Download the [latest Linux x86-64 release](https://github.com/Kthom1/switchyard/releases/latest)
and its matching SHA-256 checksum into a new, empty directory:

```bash
switchyard_download=$(mktemp -d)
cd "$switchyard_download"
switchyard_release=$(gh release view --repo Kthom1/switchyard --json tagName --jq .tagName)
switchyard_archive="switchyard-${switchyard_release}-linux_x86_64.tar.gz"
gh release download "$switchyard_release" --repo Kthom1/switchyard \
  --pattern "$switchyard_archive" --pattern "$switchyard_archive.sha256"
sha256sum -c "$switchyard_archive.sha256"
switchyard_releases="$HOME/.local/share/switchyard/releases"
mkdir -p "$switchyard_releases/$switchyard_release" "$HOME/.local/bin"
tar -xzf "$switchyard_archive" -C "$switchyard_releases/$switchyard_release"
ln -s "$switchyard_releases/$switchyard_release/switchyard/switchyard" "$HOME/.local/bin/switchyard"
export PATH="$HOME/.local/bin:$PATH"
switchyard version
```

Keep each release bundle intact in its own directory under
`~/.local/share/switchyard/releases`; the link lets you run the CLI from any
directory while it locates the bundled runner and notices. The link command
refuses to replace an existing `switchyard` command. To move to a later release,
see [Upgrade an installation](#upgrade-an-installation).

To keep it on PATH in new terminals, add this line to `~/.bashrc` for Bash or
`~/.zshrc` for Zsh, unless that directory is already on your PATH:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

For Fish, run `fish_add_path ~/.local/bin` once instead. Install the bundle and
create the link using the Bash commands above.

## Start the board

```bash
switchyard init
```

The command checks prerequisites, starts Plane and creates a local board:

- Workspace **Switchyard** (`switchyard`).
- Project **Tasks**, with task prefix **APP**.
- The required workflow states and **agent** label.
- Your local Plane owner account and an automation account for task updates.
  Git operations use your existing GitHub CLI login.

In a terminal, choose **Recommended** (the default) to add Ponytail, Compound
Engineering, Frontend Design and ShowMe in your selected Codex configuration,
or **Clean** to keep your existing setup. Repository connections are added
separately with `project add`.

Open the printed board URL and sign in as `owner@switchyard.local`. Your generated
password is in the private `~/.switchyard/plane.json` file, or `plane.json` inside
your selected `SWITCHYARD_HOME`. Open that file locally to read it. Setup prints
its location, not the password or API key.

## Connect your first repository

After `init`, connect the repository and start the runner:

```bash
switchyard project add \
  --repo https://github.com/YOUR_ACCOUNT/YOUR_REPOSITORY.git
switchyard up
```

Switchyard connects the **Tasks** project (`APP`) to this repository using the
saved Plane automation API key. The runner uses your existing Codex setup.

Create a small task with a clear check, add the **agent** label and move it to
**Todo**. Read the resulting branch and checks when it reaches **Human Review**.

## Add another repository

Use the same installation for additional repositories:

```bash
switchyard project add \
  --repo https://github.com/YOUR_ACCOUNT/YOUR_WEBSITE.git \
  --identifier WEB
switchyard project list
```

On a board created by Switchyard, this creates a private Plane project with the
required states, **agent** label and automation membership. `--identifier` is
optional; its default comes from the repository name. Each connected project
must have a unique task prefix.

The new project shares the board, owner login, Codex configuration and runner.
Each task uses the repository connected to its project. The runner handles one
task at a time across all connected projects by default.

`project add` starts the board if needed but does not start the runner. If the
runner is already running, it picks up the new mapping on its next configuration
reload. Otherwise, run `switchyard up`. Tasks in unmapped Plane projects do not
run, even if they have the **agent** label.

Repeating the same connection is safe. The command refuses to replace an
existing project's repository or identifier. Project mappings are stored in
`tracker.provider.projects` in `~/.switchyard/WORKFLOW.md`, or the workflow
inside your chosen `SWITCHYARD_HOME`.

## Connect an existing project on this board

For a project already in your Switchyard workspace, use its UUID and existing
task prefix:

```bash
switchyard project add \
  --repo https://github.com/YOUR_ACCOUNT/YOUR_REPOSITORY.git \
  --project-id YOUR_PROJECT_UUID \
  --identifier YOUR_PROJECT_PREFIX
```

The project URL contains its UUID. Its short uppercase task prefix, such as
`WEB`, is the identifier. On a board created by Switchyard, the local owner must
administer this project. Switchyard adds automation access and any missing
**Human Review**, **Blocked** and **agent** conventions. Existing states and
tasks are preserved. If you renamed other states, check that they still match
the workflow's active and terminal states.

An explicit `--project-id` also lets multiple Plane projects use the same
repository. Each still needs a unique task prefix. All connected projects use
this installation's saved Plane workspace and API key.

## Manually configured Plane boards

For a project you already set up in this installation's local Plane board,
make the first connection with its project details and Plane API key.
Use this path when the board was configured manually instead of created by
Switchyard. These options do not connect to an external Plane server.
Run `switchyard init` to prepare the installation. When Plane is already set up,
it leaves existing accounts intact; use the explicit connection below to supply
your workspace and credential.

In project settings, open **States** and ensure **Backlog**, **Todo**,
**In Progress**, **Human Review**, **Blocked**, **Done** and **Cancelled** exist.
Reuse existing states and add only the missing ones. Under **Labels**, ensure
an **agent** label exists.

Use a Plane account with access to the workspace and project; a separate
Plane automation account is optional. Check its project membership under
project settings → **Members**. Sign in as that account, open
profile settings → **Personal Access Tokens**, and choose **Add access token**.
The project URL contains the workspace slug and project UUID; its short
uppercase task prefix, such as `APP`, is the project identifier.

```bash
switchyard project add --api-key-stdin \
  --repo https://github.com/YOUR_ACCOUNT/YOUR_REPOSITORY.git \
  --workspace YOUR_WORKSPACE_SLUG \
  --project-id YOUR_PROJECT_UUID \
  --identifier YOUR_PROJECT_PREFIX < /path/to/plane-api-key
```

`--api-key-stdin` reads one key line from standard input; pipe it from your secret
store or redirect a private key file into the command. Switchyard checks that
the key can access the project and that its identifier matches before saving a
new connection. If the check fails, correct the settings or project membership
and rerun `project add`.

For additional projects on a manually configured board, prepare their states,
labels and automation membership as above, then use `switchyard project add`
with `--repo`, `--project-id` and `--identifier`. It reuses the saved API key.

## Daily commands

| Command | What it does |
| --- | --- |
| `switchyard up` | Start Plane and the runner, then wait for both to respond. |
| `switchyard status` | Show service state and URLs; report unavailable services. |
| `switchyard logs` | Show recent runner logs. |
| `switchyard logs plane --follow` | Follow the board's logs. |
| `switchyard project list` | Show connected Plane projects and repositories. |
| `switchyard project add --repo URL` | Connect another repository, creating its Plane project on a Switchyard-managed board. |
| `switchyard down` | Stop services and preserve tasks, attachments, configuration and workspaces. |
| `switchyard backup [DIRECTORY]` | Stop services and save database, attachments and configuration; run `up` to resume. |
| `SWITCHYARD_HOME=/empty/destination switchyard restore BACKUP_DIRECTORY` | Restore into an empty installation; only Postgres starts. |
| `switchyard upgrade [--wait] [--keep-backup]` | Run from a newly extracted release to move the installation to it. |
| `switchyard codex` | Run the installed Codex with your selected configuration. |

Repeated `init` calls preserve credentials and existing project mappings.
`init` does not accept repository or project connection options; use `project add`
for the first and every subsequent connection. Private installation configuration, Plane credentials
and task workspaces live in `~/.switchyard`. Plane's database
and attachments live in Docker volumes. The installation path determines the
volume names, so moving the directory or changing `SWITCHYARD_HOME` does not
move the board data. Use the [backup and restore guide](backup.md) to protect
or relocate an installation.

Set an absolute `SWITCHYARD_HOME` before every command to use another
installation:

```bash
export SWITCHYARD_HOME="$HOME/.local/share/my-switchyard"
switchyard init --port 8190 --runner-port 8191
```

The board and dashboard bind to loopback. Set `--web-url` during the first
`init` when using a different browser origin; follow the
[private access guide](operations.md#private-access). Each installation needs
its own pair of ports.

Choose the setup mode with either flag to skip the walkthrough:

```bash
switchyard init --recommended
switchyard init --clean
```

The flags are mutually exclusive. Without a flag, terminal sessions show the
walkthrough with Recommended selected by default. Nonterminal input skips the
walkthrough and installs no optional plugins or skills unless `--recommended`
is supplied. Existing configuration and installed skills are
preserved in either mode.

See [recommended plugins and skills](recommended.md) for their sources, scope
and hook controls.

If you built the CLI separately, use
`switchyard init --runner /absolute/path/to/extracted/switchyard/bin/symphony`.
Keep the complete runner bundle at that path so its notices can be installed.

To start services again after `down`, run `up`.

## Upgrade an installation

Download and verify the new release as in [Install on your PATH](#install-on-your-path),
extract it into its own directory, and run the upgrade with that release's CLI:

```bash
switchyard_download=$(mktemp -d)
cd "$switchyard_download"
switchyard_release=$(gh release view --repo Kthom1/switchyard --json tagName --jq .tagName)
switchyard_archive="switchyard-${switchyard_release}-linux_x86_64.tar.gz"
gh release download "$switchyard_release" --repo Kthom1/switchyard \
  --pattern "$switchyard_archive" --pattern "$switchyard_archive.sha256"
sha256sum -c "$switchyard_archive.sha256"
switchyard_releases="$HOME/.local/share/switchyard/releases"
mkdir -p "$switchyard_releases/$switchyard_release"
tar -xzf "$switchyard_archive" -C "$switchyard_releases/$switchyard_release"
"$switchyard_releases/$switchyard_release/switchyard/switchyard" upgrade
```

`upgrade` works through these steps and stops at the first problem:

1. It refuses a runner service you edited by hand or generated from a changed
   template, as `up` does, and refuses while the runner has running or retrying
   tasks. Add `--wait` to wait until it is idle instead.
2. It stops the runner and takes a backup of the board's data, attachments and
   configuration, which stops Plane, then checks your files and the service again.
3. It replaces release-owned files: the runner, scripts, deploy files, guides and
   licenses. A file you changed is kept, and the release's copy is written beside
   it as `FILE.new` for you to review. Files the installed release shipped and the
   new one does not are removed unless you changed them. A linked file, or
   anything in a linked directory, is yours: it is never written or removed.
4. It regenerates the runner's service when the release changed its template,
   starts Plane and the runner, and verifies the installed build, both services
   and your project connections.
5. It records the new build in `BUILD.txt`. Until then, nothing is final.

If any step fails, it restores every file it changed, starts the previous release
again and deletes the backup. The exception is a release that changed Plane's
images: once it has started, Plane may have migrated its data, so the previous
release is left stopped and the backup is kept for a restore as in
[Backups](backup.md). Until you restore it, `up` and `upgrade` refuse and name
`work/upgrade-plane-migrated`; delete that file only if Plane's data was not
migrated.

After a verified upgrade it deletes the backup (add `--keep-backup` to keep it)
and points `~/.local/bin/switchyard` at the new release. In
`~/.local/share/switchyard/releases`, it removes releases whose version
directories (such as `v0.3.1`) are older than the previous release, and their
extracted runner runtimes. A release whose runtime a running runner, such as
another installation's, still uses is kept until a later upgrade can remove both. The previous release stays available for a quick
rollback; directories it cannot place by version are kept, as is the release you
upgrade to.

Do not edit the runner service or release files while an upgrade runs; changes
made before it starts are kept as described above.

If an upgrade is interrupted, run `switchyard upgrade` again from any release.
Before anything else it finishes an upgrade that recorded its build, or restores
the previous release, once the runner is idle; then it does what you asked. Only
one upgrade runs at a time, and `init`, `project`, `up`, `down` and `backup` wait
their turn: they refuse while an upgrade runs or one has not finished.

It never changes `config.json`, `.env`, `.env.plane`, `plane.json`, `WORKFLOW.md`,
your Codex or Claude configuration, installed skills, or task workspaces. Task
workspaces are also outside the backup, so push or save any unfinished work you
need before upgrading.

Installations from releases before v0.3.2 have no record of their original
files. `upgrade` compares them with the release bundle that
`~/.local/bin/switchyard` points to; a file it cannot verify is treated as changed
and kept. A bundle extracted directly into `~/.local/share/switchyard` is not
removed automatically; delete it after a successful upgrade.
