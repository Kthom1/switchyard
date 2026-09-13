---
name: switchyard
description: >
  Coordinate authorized coding work through a configured Switchyard and
  Symphony runner: capture, dispatch, monitor, steer, review, and hand off
  tasks. Also defines how an assigned worker implements its own task without
  requeueing it.
---

# Switchyard

Use Switchyard as the execution path, not only as a task board.

## Choose the role

- A **conversational orchestrator** scopes work, queues authorized tasks through
  the configured Symphony runner, monitors and steers workers, reviews results,
  and completes only the authorized delivery steps.
- An **assigned worker** implements its current task in its provided checkout.
  It must not enqueue or delegate the same task because this skill was loaded.

Keep the role established by the current conversation or assignment. Use another
execution path only for a concrete reason, with ownership and the stopping point
stated first.

## Discover the configured path

Before dispatching, inspect the installation instead of assuming its location or
connection details:

1. Run `switchyard status` and `switchyard project list` in the configured
   environment. Use `switchyard --help` and the installed documentation when the
   commands differ.
2. Confirm the target Plane project maps to the intended repository and that this
   mapping is consumed by the active Symphony runner. A Plane project by itself
   is not a runner connection.
3. Use the configured Plane client or board for task operations. Keep credentials
   in the installation; do not copy tokens into task text, prompts, or worker
   configuration.

If the repository is not mapped or the runner is unavailable, report that gap.
Do not invent a second queue or runner.

## Orchestrate the work

1. **Deduplicate and bound it.** Search existing tasks before creating one. State
   the outcome, settled context, scope, exclusions, acceptance checks,
   dependencies, stopping point, and owners for implementation, independent
   review, integration, browser evidence when needed, and delivery.
2. **Respect capture and authorization.** Capture-only work stays in Backlog
   without the `agent` label. Dispatch only work already authorized for execution;
   do not ask again for authorization that the user already gave.
3. **Queue independent work.** Put an authorized task in the mapped project using
   the installation's active state and required label, normally Todo plus `agent`.
   Keep dependent tasks inactive until their prerequisite revision is available.
4. **Confirm pickup.** Check that the intended task and run become active and
   produce real activity. A board update, process, token count, or completion
   event alone does not prove the assigned worker is progressing.
5. **Monitor evidence.** Inspect task activity, source changes, runner logs,
   checks, and handoffs. Tell the worker which unchanged checks already passed.
   After a narrow correction, request only the affected check.
6. **Steer the active worker.** Consolidate corrections into the current brief,
   then verify acknowledgement and changed behavior. A board comment preserves
   history but does not prove a running model read it. If necessary, use the
   installation's supported stop and requeue process while preserving the same
   checkout and work; never create a competing coding task.
7. **Stop waste and diagnose first.** Intervene when work repeats, drifts, or
   stalls. For repeated failures, isolate whether the source, fixture, locator,
   or harness is wrong and get one decisive reproduction before requesting a
   product change. Route the bounded correction to the owning worker.
8. **Review identity and proof.** Match completion to the intended task and run.
   Require a committed branch, exact revision, relevant check results, and known
   limitations. Preserve review and delivery evidence before cleanup.
9. **Honor the delivery boundary.** Human Review means ready for review, not
   merged, released, or deployed. Perform only delivery steps explicitly
   authorized by the user.

## Assigned-worker behavior

Implement the assigned task in the provided isolated checkout. Read the task and
existing comments, preserve unrelated work, make the smallest complete change,
run relevant checks, and provide the branch, exact revision, results, and
limitations at the requested stopping point. If blocked, preserve the checkout
and report the concrete next action. Do not change runner configuration or
recursively queue the assignment.

## Scenario checks

- **Capture:** "Do this tomorrow" becomes unlabelled Backlog work and does not run.
- **Mapped dispatch:** authorized coding work enters the mapped project and is
  picked up by its Symphony worker.
- **Correction:** one consolidated correction reaches that worker, with
  acknowledgement or a preserved stop/requeue; no duplicate task is created.
- **Review and delivery:** the handoff names branch, revision, checks, and limits,
  stops at Human Review, and crosses later delivery boundaries only when allowed.
- **Assigned worker:** the worker implements its current task rather than
  requeueing or delegating it.
