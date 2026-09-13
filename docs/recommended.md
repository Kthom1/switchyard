# Recommended plugins and skills

The recommended set adds Ponytail, Compound Engineering, Frontend Design and
ShowMe to your selected Codex configuration. The interactive `switchyard init` walkthrough
defaults to Recommended; choose Clean to skip them.

## Install

Follow [CLI setup](cli.md) to put `switchyard` on your PATH, then run `init` and
accept Recommended. To skip the walkthrough and select the complete set directly:

```bash
switchyard init --recommended
```

Use `--clean` to skip the walkthrough without installing optional plugins or skills.
It preserves anything already installed. Connect repositories afterward with
`switchyard project add --repo URL`, as described in [CLI setup](cli.md).

Ponytail includes hooks that can apply its guidance throughout a session. Read
[Ponytail hooks](#ponytail-hooks) before enabling them.

### Install the Switchyard orchestration skill

The repository includes a separate [Switchyard skill](../skills/switchyard/SKILL.md)
for conversational agents that scope, dispatch, monitor and review worker tasks.
It is independent of the optional Recommended set above. From a Switchyard source
checkout or extracted release, install it in the Codex home used by the
orchestrator:

```bash
codex_skills="${CODEX_HOME:-$HOME/.codex}/skills"
mkdir -p "$codex_skills"
test ! -e "$codex_skills/switchyard" || {
  echo "switchyard skill already exists; review it before replacing" >&2
  exit 1
}
cp -R skills/switchyard "$codex_skills/"
codex
```

Open `/skills` and confirm `switchyard` is listed, then start a new session. Do
not replace an existing copy without reviewing its local changes first.

The plugins and skills use your normal Codex home, usually `~/.codex`, or your
configured `CODEX_HOME`. They are available to your interactive Codex sessions
as well as Switchyard. Use the same `CODEX_HOME` when installing and running.
Codex also discovers user-wide `.agents/skills`, repository skills and
administrator skills. Open `codex` and inspect `/skills` to see what
is available to the execution agent.

| Recommendation | Adds | Version and source | License |
| --- | --- | --- | --- |
| Ponytail | Simplicity and code review skills, plus optional lifecycle hooks. | [Ponytail 4.9.0](https://github.com/DietrichGebert/ponytail/tree/356918eba965ee1eac64bd3a7f0dd02108350de5), commit `356918eba965ee1eac64bd3a7f0dd02108350de5`. | [MIT](https://github.com/DietrichGebert/ponytail/blob/356918eba965ee1eac64bd3a7f0dd02108350de5/LICENSE) |
| Compound Engineering | Planning, implementation, review and documentation skills. | [Compound Engineering 3.24.0](https://github.com/EveryInc/compound-engineering-plugin/tree/8df67793b9733d2220fa9a7fc37139931471af62), commit `8df67793b9733d2220fa9a7fc37139931471af62`. | [MIT](https://github.com/EveryInc/compound-engineering-plugin/blob/8df67793b9733d2220fa9a7fc37139931471af62/LICENSE) |
| Frontend Design | Anthropic's individual `frontend-design` skill. | [Frontend Design](https://github.com/anthropics/skills/tree/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/frontend-design), commit `41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f`. | [Apache-2.0](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/frontend-design/LICENSE.txt) |
| ShowMe | Visual explanations using diagrams, code sketches and focused HTML. | [ShowMe](https://github.com/humanlayer/skills/tree/3c2629142c5d437428269b1b722b08c0b87f574d/plugins/show-me/skills/show-me), commit `3c2629142c5d437428269b1b722b08c0b87f574d`. | [MIT](https://github.com/humanlayer/skills/blob/3c2629142c5d437428269b1b722b08c0b87f574d/LICENSE) |

Start a new runner session after installation. Rerunning the installer keeps the
same plugin IDs and preserves existing `frontend-design` and `show-me` skills in
the selected Codex home or user-wide `.agents/skills`. A preserved skill may use a
different version. For other skill locations, check `/skills` before installing
another copy.

## Ponytail hooks

Ponytail's `SessionStart`, `UserPromptSubmit` and `SubagentStart` hooks require
Node.js on the runner's PATH. Once trusted, they can activate Ponytail's default
`full` mode at session start and apply its guidance to prompts and subagents.

To enable them, open `codex`, inspect `/hooks`, and review and trust
the commands through Codex. Leave them untrusted to use only individual skills.
See [Codex plugin controls](https://learn.chatgpt.com/docs/plugins) for hook trust.

Use `stop ponytail` to stop its guidance for the current session. The explicit
`/ponytail default ...` command changes user-wide XDG configuration, independently
of the selected Codex home. Uninstalling Ponytail removes its hooks from future runner sessions.

## Manage and remove

```bash
codex plugin marketplace list
codex plugin list
codex                     # Open /skills, /plugins or /hooks.
codex plugin remove ponytail@ponytail
codex plugin remove compound-engineering@compound-engineering-plugin
```

The installer uses the pinned sources above and does not check for updates.
Use `/plugins` to manage installed plugins. To remove a standalone skill, remove
`skills/frontend-design` or `skills/show-me` inside the selected Codex home.
If `/skills` shows an inherited user-wide copy, manage it at its displayed location.
