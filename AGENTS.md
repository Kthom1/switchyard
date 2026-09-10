# Switchyard

Plane owns tasks and durable progress. Symphony owns scheduling, retries, isolated
workspaces, and the dashboard. Add only the adapter and deployment glue here.

- Read the closest upstream implementation before changing an integration.
- Do not build another scheduler, task database, or dashboard.
- Never run agent work in this source checkout. Use the configured workspace root.
- Keep Plane tokens host-side. The tool accepts task UUIDs, not arbitrary URLs.
- Backlog is unapproved work. Only tasks labelled `agent` in Todo or In Progress run.
- Human Review and Blocked stop execution but preserve workspaces.
- Do not merge or deploy task branches without explicit approval.
- Never commit .env files, credentials, runtime logs, or generated workspaces.
- Follow CONTRIBUTING.md in fresh or sandboxed task checkouts, then run scripts/check after adapter changes. Keep setup and recovery commands current.
