# Operating Switchyard

[← Switchyard](../README.md) · [Getting started](cli.md)

## Run in the background

After setup, start the board and runner with:

```bash
switchyard up
switchyard status
switchyard logs
```

The CLI creates a systemd user service for this installation. To let the service
start before login, run `loginctl enable-linger "$USER"` once. Use
`switchyard down` to stop services while preserving their data.

The service saves its initial PATH and the selected absolute Codex home as
`CODEX_HOME`, including the default `~/.codex`.
Repeated `up` calls retain them when run from another terminal. To change the
saved environment, run `down`, remove only that installation's unit file shown
in the service error or systemd output, then run `up` from the intended environment.

An older service may explicitly select the installation's `work/codex` directory
through `SWITCHYARD_CODEX_HOME`. Recreate that stopped unit as above. To use your
normal Codex configuration, unset both home variables and check `codex login status`
before running `up`. To retain the old separate home, export `CODEX_HOME` with
its absolute path before running `up`. Changing the service does not move or
delete either directory.

The default installation is `~/.switchyard`. Keep its path stable; if you set
`SWITCHYARD_HOME`, use that same value for every command.

## Private access

Plane and the runner dashboard bind to loopback. On the same computer, open
[the board](http://localhost:8090) and [the dashboard](http://localhost:8091)
directly. No VPN or second computer is needed.

The dashboard has no application login. Keep both services off the public
internet and limit remote access to trusted users.

### SSH tunnel

On the computer with your browser, connect to the Linux machine running Switchyard:

```bash
ssh -L 8090:127.0.0.1:8090 -L 8091:127.0.0.1:8091 YOUR_LINUX_HOST
```

Keep the connection open and use the localhost links above. Plane's browser
origin and the runner's `--web-url` remain `http://localhost:8090`.

### Tailscale

If both computers already use Tailscale, run these commands on the Linux machine:

```bash
tailscale serve --bg --https=443 http://127.0.0.1:8090
tailscale serve --bg --https=8443 http://127.0.0.1:8091
```

For a new installation, use the HTTPS address printed by the first command in
`switchyard init --web-url YOUR_HTTPS_ORIGIN`. Project connections use that saved browser origin.
The API endpoint can remain `http://127.0.0.1:8090`. Restrict access to trusted
users on your tailnet. Tailscale Serve provides private access; do not use Funnel.

For an existing installation, the generators retain its current configuration.
In `~/.switchyard/.env.plane` (or your `SWITCHYARD_HOME`), set `APP_DOMAIN` to the new hostname and optional port, and set
`WEB_URL` and `CORS_ALLOWED_ORIGINS` to the new HTTPS origin. Set
`tracker.provider.web_url` in `WORKFLOW.md` and `web_url` in `config.json`
to that same origin. Run `switchyard down` and `switchyard up` to apply it.

## Queue, stop and review

| State | Runner behavior |
| --- | --- |
| Backlog | No execution. |
| Todo + agent label | Ready for the next worker. |
| In Progress + agent label | Active or eligible to resume. |
| Human Review / Blocked | Stop; preserve the checkout and branch. |
| Done / Cancelled | Stop; permit task checkout cleanup. |

Removing the label or leaving active states stops a task after the next
successful reconciliation poll. Tracker failures can delay that stop. For an
independent stop, run `switchyard down`.

The agent's Plane tool reads within its assigned project and can update only
its assigned active task. A state change can race an
in-flight request, so a write already underway may complete after you stop it.
Marking a task Done does not guarantee immediate cleanup; Symphony also removes
terminal task workspaces during startup.

There is one shared runner, with one concurrent agent across all connected
projects by default. Tasks must be independent; Plane blocking relationships
are not interpreted.

## Projects and repositories

A Plane project is the unit of repository routing. Connect it with
`switchyard project add --repo URL`, or provide `--project-id` and `--identifier`
for an existing project. See the [connection examples](cli.md#add-another-repository).
Use `switchyard project list` to see what is connected. Creating a project in
Plane alone does not make its tasks eligible to run.

Mappings live in the host's `WORKFLOW.md`, under `tracker.provider.projects`.
For example, this portion of the configuration connects two projects:

```yaml
tracker:
  provider:
    projects:
      - project_id: 11111111-1111-4111-8111-111111111111
        project_identifier: APP
        repo: https://github.com/YOUR_ACCOUNT/YOUR_APP.git
      - project_id: 22222222-2222-4222-8222-222222222222
        project_identifier: WEB
        repo: https://github.com/YOUR_ACCOUNT/YOUR_WEBSITE.git
```

The endpoint, workspace and API key are shared settings alongside this list.
Each project maps to one repository and has a unique identifier. Task text,
comments and labels cannot select a different repository. The bundled hooks
check the task, project, repository origin and agent branch before resuming a
mapped checkout. Custom hooks should preserve those checks.

Adding a mapping preserves other workflow settings and the task prompt. A
running service notices the updated workflow without a restart. The CLI refuses
to overwrite an existing connection. Before manually changing or removing a
mapping, run `switchyard down` and retain any unfinished task checkouts.

Moving a task between projects, or changing its repository mapping, stops its
current run after reconciliation. Its old checkout is preserved. If the task
remains eligible in the destination project, the runner may start it there.
Move it to **Blocked** first when you want to review the change before resuming.
An existing checkout with a different origin or task identity is refused rather
than reused; retain that work before choosing a fresh checkout location.

### Existing single-project workflows

The earlier `project_id` and `project_identifier` fields plus `.env`'s
`SOURCE_REPO_URL` remain supported. Their existing checkouts can still resume.
Adding another project converts the saved workflow to the mappings list.
Old checkouts without recorded task identities are preserved and are not
automatically adopted by the mapped workflow.

Before expanding an older installation, run `switchyard down`, preserve any
unfinished checkouts, and set `SYMPHONY_WORKSPACE_ROOT` in its `.env` to a new,
empty directory outside Git repositories. Then add the project and run
`switchyard up`. Keep the old directory for review. With mapped projects,
repository URLs come from `WORKFLOW.md`; `SOURCE_REPO_URL` is only the legacy
fallback.

## Retries and usage

Retry state, operator-input blocks and dashboard counters live in memory. Active
tasks may resume after a restart, so the workflow asks agents to read existing
comments before writing.

For an operator-input block, resolve the cause, let the task leave active routing
for a successful poll, then requeue it. Alternatively, restart after resolving
the cause. Moving only between Todo and In Progress may not clear the block.

`max_turns` limits an invocation, not lifetime retries. Turn and stall timeouts
measure inactivity, not total elapsed runtime; continuous output can keep a run
alive. These settings do not cap total usage or cost. Park repeatedly failing
tasks in Blocked, or stop the runner.

## Credentials

Switchyard uses the installed Codex and your existing configuration and login.
Use `codex login status` to check authentication and `codex login --device-auth`
when sign-in is needed. The standard `CODEX_HOME` selects a different Codex
configuration; run setup and `up` from the environment you intend to use.
The workflow still sets the approval and sandbox policies for unattended tasks.
Your configured MCP servers, plugins, skills, hooks and account apps retain
their normal behavior. See Codex's
[configuration](https://developers.openai.com/codex/config-advanced/) and
[authentication](https://developers.openai.com/codex/auth/) guides.

Symphony removes the declared Plane token variable from the Codex child and runs
the bundled Plane tool on the host. That tool is scoped to the assigned project
and task. Additional tools from your Codex configuration have their own access
and may be broader. Codex runs as your Linux user; use trusted repositories and
task authors.

Preserve the database, uploads and private configuration before upgrades.
`switchyard backup` captures the database, uploads and private configuration;
follow the [backup and recovery guide](backup.md). `switchyard down` preserves
the Docker volumes but does not create a backup. Do not run
`docker compose down -v` on an installation you need.
