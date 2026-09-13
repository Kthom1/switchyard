---
name: switchyard
description: >
  Orchestrate authorized coding work through a configured Switchyard and
  Symphony runner: discover the mapped path, dispatch Plane tasks, verify and
  steer workers, and review and hand off results.
---

# Orchestrate with Switchyard

Use Switchyard to orchestrate coding work through the configured Symphony runner,
not only as a task board.

## Discover the configured path

Before dispatching, inspect the installation instead of assuming its location or
connection details:

1. Run `switchyard status` and `switchyard project list` in the configured
   environment. Use `switchyard --help` and the installed documentation when the
   commands differ.
2. Read the installation-specific connection guidance and its current
   `WORKFLOW.md` and settings. Confirm the project mappings, required labels,
   active and terminal states, and workspace root; keep endpoint, host, and
   access values in that local guidance.
3. Confirm the target Plane project maps to the intended repository and that this
   mapping is consumed by the active Symphony runner. A Plane project by itself
   is not a runner connection.
4. Use an authorized management Plane client or signed-in board for queue-wide
   task operations. The runner's Plane tool is scoped to its assigned project and
   task and cannot manage an arbitrary orchestration queue. Keep credentials out
   of task text, prompts, and worker configuration.

If the repository is not mapped or the runner is unavailable, report that gap.
Do not invent a second queue or runner.

## Run the orchestration loop

1. **Deduplicate and bound it.** Search existing tasks before creating one. State
   the outcome, settled context, scope, exclusions, acceptance checks,
   dependencies, stopping point, and owners for implementation, independent
   review, integration, browser evidence when needed, and delivery.
2. **Keep the capture boundary.** Capture-only work stays in Backlog without the
   `agent` label. Dispatch only work already authorized for execution.
3. **Queue independent work.** Put an authorized task in the mapped project using
   the installation's active state and required label, normally Todo plus `agent`.
   Keep dependent tasks inactive until their prerequisite revision is available;
   when integration is necessary, record that revision and the feature commit
   boundary.
4. **Confirm pickup.** Check that the intended task and run become active and
   produce real activity. A board update, process, token count, or completion
   event alone does not prove the assigned worker is progressing.
5. **Monitor evidence.** Inspect task activity, source changes, runner logs,
   checks, and handoffs. When correcting the work, identify unchanged checks that
   already passed and request only affected checks.
6. **Steer the active worker.** Consolidate corrections into the current brief,
   then verify acknowledgement and changed behavior. A board comment preserves
   history but does not prove a running model read it. To stop safely, normally
   move to Blocked or Human Review so work is preserved; do not use Done or
   Cancelled to pause because they allow cleanup. Wait for a successful
   reconciliation and verify the intended run is absent before requeueing the
   same task and checkout. A state update alone is not proof of a stop, and
   tracker failures can delay it. Follow the installed version's
   `docs/operations.md` for the authoritative procedure; never create a competing
   coding task.
7. **Stop waste and diagnose first.** Intervene when work repeats, drifts, or
   stalls. For repeated failures, isolate whether the source, fixture, locator,
   or harness is wrong and get one decisive reproduction before requesting a
   product change. Route the bounded correction to the owning worker. Before a
   browser workaround, inspect installed CLI capabilities and official
   documentation. Bound any extra-tool trial and adopt it only when it
   demonstrates a concrete benefit.
8. **Review identity and proof.** Match completion to the intended task and run.
   Require a committed branch, exact revision, relevant check results, and known
   limitations. Preserve review and delivery evidence before cleanup.
9. **Keep the delivery boundary.** Human Review means ready for review, not
   merged, released, or deployed. Perform only delivery steps explicitly
   authorized by the user.
