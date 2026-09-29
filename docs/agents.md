# Other coding agents

[← Switchyard](../README.md) · [Operating guide](operations.md)

Switchyard runs Codex by default. It can instead run any coding agent that speaks
the [Agent Client Protocol](https://agentclientprotocol.com) (ACP), such as
Claude Code. The board, workflow, task checkouts, retries and dashboard stay the
same. Only the agent process changes.

## How it works

When `acp.command` is set in `WORKFLOW.md`, the runner starts that command in the
task checkout and talks ACP (version 1) over its standard input and output. Each
worker opens one ACP session; continuation turns reuse it.

- **Task tool.** The `plane` tool reaches the agent as an MCP server. It relays
  each call back to the runner over a private socket, so the Plane token stays on
  the host, as with Codex. The relay needs Python 3 (`python3`) on the host.
- **Permissions.** Runs are unattended. The agent should use a mode that does not
  ask for permission. If it still asks, the runner allows that one action and never
  saves a rule.
- **Sandbox.** ACP does not define one. `scripts/agent-sandbox` runs the agent
  with the machine read-only, except the task checkout, `/tmp` and paths you name.
  It requires [bubblewrap](https://github.com/containers/bubblewrap) and
  unprivileged user namespaces.

## Use Claude Code

Install Node.js, Python 3 and bubblewrap. Then, from your Switchyard directory
(`~/.switchyard` for a CLI installation), install the pinned Claude adapter and
sign in. The runner keeps its own Claude configuration under `work/claude`,
separate from your normal Claude settings. To use another directory, set an
absolute `SWITCHYARD_CLAUDE_HOME` in the runner's `.env`, where the service reads
it, and export the same value when running `scripts/claude-runner` yourself.

```bash
scripts/claude-runner install
scripts/claude-runner auth login
scripts/claude-runner auth status
```

Add this to the front matter of your `WORKFLOW.md`. The `codex` timeouts still
apply to every agent.

```yaml
acp:
  command: '"$SWITCHYARD_ROOT/scripts/claude-runner" acp'
  session_meta:
    claudeCode:
      options:
        settingSources: [project]
        strictMcpConfig: true
  config_options:
    mode: bypassPermissions
```

`scripts/claude-runner acp` starts the adapter inside the sandbox, with the
runner's Claude home writable so sign-in can refresh. `settingSources: [project]`
loads the repository's `CLAUDE.md` and `.claude/settings.json` but not user
settings. `strictMcpConfig` ignores MCP servers from configuration files.
`bypassPermissions` skips prompts; the sandbox limits where the agent can write.

Run `switchyard down` and `switchyard up` after changing the agent. `switchyard up`
checks the Codex sign-in only when no `acp` command is set. Remove the `acp` block
to return to Codex.

[Backups](backup.md) do not include the Claude adapter or runner home. After a
restore, run `scripts/claude-runner install` and then `scripts/claude-runner auth login`.

## Use another ACP agent

Point `acp.command` at the agent's ACP adapter, wrapped in the sandbox with any
state directory it needs to write:

```yaml
acp:
  command: '"$SWITCHYARD_ROOT/scripts/agent-sandbox" --writable "$HOME/.agent-state" -- agent-acp'
  config_options:
    mode: YOUR_UNATTENDED_MODE
```

- `session_meta` is sent as `_meta` when the session starts. Its meaning is
  agent-specific.
- `config_options` sets ACP session options, such as the mode, after the session
  starts. Option IDs and values are agent-specific.
- `read_timeout_ms` limits start-up requests. The default is 60 seconds.

Sign the agent in before dispatching tasks. A session that needs sign-in fails
and is retried like other failures.

## Limitations

- ACP agents run on the local machine. SSH worker hosts support only Codex.
- Token totals on the dashboard come from usage reported when a turn finishes.
  The runner stops the agent as soon as it moves a task to Human Review, usually
  before that report, so ACP runs often show no totals. ACP has not yet
  standardized these numbers.
- Some adapters ignore MCP servers. The Pi adapter does, so Pi cannot use the
  `plane` tool yet.
- As with Codex, use trusted repositories and task authors. The sandbox limits
  writes; it does not stop network access or reading files your user can read.
