# Get started with the CLI

[← Switchyard](../README.md) · [Build from source](../CONTRIBUTING.md)

Run Switchyard on a private **Linux x86-64** machine with:

- Docker Engine and Docker Compose 2.24.4 or later, accessible to your user.
- Git, GitHub CLI (`gh`), Codex CLI and Bash.
- A systemd user session.
- The build and test tools needed by your repositories.

Set up Codex on this machine with the configuration, skills and tools you want
it to use. Switchyard runs that installed Codex with your existing login and
configuration, including a custom `CODEX_HOME` if you use one.

Check your existing logins with `gh auth status` and `codex login status`.
If needed, sign in with `gh auth login` and `codex login --device-auth`.
Configure Git's `user.name` and `user.email` for commits. No separate GitHub
account or Switchyard-specific Codex login is required.
The release bundle includes the Symphony runner, Erlang/OTP and Elixir.

## Install on your PATH

Download the [v0.1.0 Linux x86-64 bundle](https://github.com/Kthom1/switchyard/releases/download/v0.1.0/switchyard-v0.1.0-linux_x86_64.tar.gz)
and its [SHA-256 checksum](https://github.com/Kthom1/switchyard/releases/download/v0.1.0/switchyard-v0.1.0-linux_x86_64.tar.gz.sha256),
then run these commands from the download directory:

```bash
sha256sum -c switchyard-v0.1.0-linux_x86_64.tar.gz.sha256
mkdir -p "$HOME/.local/share" "$HOME/.local/bin"
tar -xzf switchyard-v0.1.0-linux_x86_64.tar.gz -C "$HOME/.local/share"
ln -s "$HOME/.local/share/switchyard/switchyard" "$HOME/.local/bin/switchyard"
export PATH="$HOME/.local/bin:$PATH"
switchyard version
```

Keep the bundle together in `~/.local/share/switchyard`; the link lets you run
the CLI from any directory while it locates the bundled runner and notices.
The link command refuses to replace an existing `switchyard` command.

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
