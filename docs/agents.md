# Agent integration

## Install and choose a mode

Run the installer from the lsops binary you intend to keep. It records that
executable's absolute path and the selected config's absolute path.

```sh
lsops hook install claude --mode output
lsops hook install codex --mode instructions
lsops hook install all --mode output
```

Supported target names: `claude`, `codex`, `gemini`, `cursor`, `copilot`, `opencode`, `omp`.
`all` configures all seven, even if a harness is not installed. It processes each
independently, reports any failure, and returns a nonzero status if one fails.

The default scope is user-wide. To configure the current project or an explicit
project directory:

```sh
lsops hook install claude --scope project --mode output
lsops hook install claude --dir /absolute/project --mode output
```

A chosen config remains explicit regardless of which project launches the hook:

```sh
lsops --config /absolute/catalogue.json hook install claude --mode output
```

These integrations are local to the machine where installed. Project hooks use
absolute local paths; another machine must install its own matching integration.

## Instructions and direct output

`instructions` is the default. The injected text tells the agent how to invoke
the selected binary/config and to consult a CLI's native help before unfamiliar
usage. It does not enumerate tools or require a valid catalogue to render.

`output` injects the current plain-text output of `lsops`. It adds no separate
agent instruction or context preamble. Harnesses that require JSON receive that
same text inside their documented context field. OpenCode and OMP receive plain text through their plugins. Hooks never opt into automatic PATH discovery.

Inspect either payload locally:

```sh
lsops hook render --mode instructions
lsops hook render --mode output
```

Change mode by rerunning the install command. Reinstallation updates only the
lsops-owned entry. `--limit N` is optional; zero, the default, includes every
configured CLI. A positive entry limit reports omissions in the payload.
Individual harnesses can additionally limit or spill large context output.

## Refresh behavior

The installed hook is a live command, not a copied inventory. Every event starts
the executable again and reads the current config. Adding, removing, or editing
an entry takes effect at the next supported event. Replacing the binary at the
same path likewise takes effect on its next invocation. No watcher or background
service is needed. Already-delivered conversation context cannot be retroactively
edited by lsops.

| Harness | Installed events/API | After compaction |
| --- | --- | --- |
| Claude Code | `SessionStart`, matching `startup`, `resume`, `clear`, `compact`, `fork` | The `compact` source reinjects the catalogue. |
| Codex CLI | `SessionStart` with the same sources | The `compact` source runs before the immediate next model request, including mid-turn automatic compaction. |
| Gemini CLI | `SessionStart` and `BeforeAgent` | Refreshes on the next submitted user turn. No immediate mid-turn post-compaction guarantee. |
| Cursor | `sessionStart` | Startup only; this is asynchronous, so injection before the very first request is not guaranteed. |
| GitHub Copilot CLI | `sessionStart` | Startup only; no documented post-compaction context event. |
| oh-my-pi (OMP) | `session_start`, `session_switch`, `session_branch`, `session_tree`, `session_compact` | Adds a fresh catalogue on the post-compaction event, not on ordinary model requests. |
| OpenCode | V2 `context`; V1 `experimental.chat.system.transform` | Fresh context is added to each outgoing model request, including the first continuation after compaction. |

`PreCompact`/`PreCompress` notifications are not installed as pretend
post-compaction injection hooks. No integration rewrites user prompts, starts an
extra model turn, or invokes the registered CLIs.

### Harness requirements

- Codex requires review/trust of new or changed hook definitions in `/hooks`;
  project hooks also require project trust. lsops configures files without editing
  trust records. Disabled hooks or a managed-only policy can still prevent use.
- Claude and Gemini keep their existing hook enablement and workspace trust
  requirements. Existing disable settings are preserved and reported when seen
  in the edited file.
- Cursor's hook timing is asynchronous. Its Windows desktop shell is documented
  as PowerShell; native Windows CLI parity has not been runtime-tested here.
- Copilot's integration uses its documented direct `exec`+`args` form and expects
  the current CLI hook API. Cloud-agent support is not claimed.
- OpenCode uses a dual-format JavaScript plugin for V2 and V1 1.18.29 or later.
  Older V1 function-only plugin loaders are unsupported. The adapter invokes
  lsops with an argument array, no shell, a 10-second timeout, and a 256 KiB output
  bound. It adds output to each outgoing request, not persisted message history.

The release targets the linked current APIs. Older harness versions may require
an update. No authenticated live harness session was used for validation;
transport, generated commands, and plugin callbacks are tested separately.

## Files and environment overrides

| Target | Default user file | Project file |
| --- | --- | --- |
| Claude | `~/.claude/settings.json` | `.claude/settings.json` |
| Codex | `~/.codex/hooks.json` | `.codex/hooks.json` |
| Gemini | `~/.gemini/settings.json` | `.gemini/settings.json` |
| Cursor | `~/.cursor/hooks.json` | `.cursor/hooks.json` |
| Copilot | `~/.copilot/hooks/lsops.json` | `.github/hooks/lsops.json` |
| OMP | `~/.omp/agent/extensions/lsops.js` | `.omp/extensions/lsops.js` |
| OpenCode | `~/.config/opencode/plugins/lsops.js` | `.opencode/plugins/lsops.js` |

For user scope, `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, and `COPILOT_HOME` override the
corresponding roots. OpenCode honors `OPENCODE_CONFIG_DIR` first, then `XDG_CONFIG_HOME/opencode` when set. The
lsops catalogue path separately follows `--config`, `LSOPS_CONFIG`, and the
platform defaults documented in the README.

The installer merges only entries marked `--managed-lsops-hook`. Unrelated
settings and hooks survive. It makes a unique sibling backup of the exact
previous bytes before changes, then writes atomically. Backup suffixes do not
look like active hook/plugin files. Repeating the same installation leaves the
file unchanged.

Malformed JSON, duplicate keys, JSONC comments, incompatible hook structures,
and symlinked destinations are rejected unchanged. lsops does not remove an
unmarked file named `lsops.js`, or an old manually installed 0.1 hook without the
management marker. Remove that old hook manually if migrating to managed hooks.

## Inspect or remove

```sh
lsops hook targets
lsops hook status all
lsops hook preview claude --mode output
lsops hook uninstall claude
lsops hook uninstall all --scope project
```

`status` reports whether configuration is present. It cannot prove a harness has
loaded or approved the hook. `preview` prints the proposed standalone JSON or
OpenCode or OMP plugin without touching files. Removal preserves unrelated entries and
backs up previous bytes. Reload/restart the harness if it does not automatically
reload its configuration.

If you relocate the binary, choose another config path, change mode, or change
an explicit entry limit, run the installer again. Ordinary catalogue edits need
no installation step. Agent runtime PATH differences can affect availability
annotations; explicit executable paths avoid reliance on a shell's PATH.

## Protocol sources

Primary documentation links supplied with version 0.2.0 (not a claim of a new authenticated harness validation):

- [Claude Code hooks](https://code.claude.com/docs/en/hooks): command/args exec form,
  SessionStart sources, and `hookSpecificOutput.additionalContext`.
- [Codex hooks](https://developers.openai.com/codex/hooks/): configuration locations,
  trust, SessionStart compact timing, Windows override, and output limits.
- [Gemini CLI hooks reference](https://geminicli.com/docs/hooks/reference/):
  SessionStart and BeforeAgent context, and the limitations of compression events.
- [Gemini hook runner](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/hooks/hookRunner.ts)
  and [shell selection](https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/utils/shell-utils.ts):
  platform-specific command execution.
- [Cursor hooks](https://cursor.com/docs/hooks): sessionStart additional_context,
  asynchronous timing, and observational preCompact behavior.
- [GitHub Copilot hooks reference](https://docs.github.com/en/copilot/reference/hooks-reference):
  direct executable hooks, user/project locations, and sessionStart additionalContext.
- [OpenCode plugin API](https://opencode.ai/v2/docs/build/plugins/): context hooks,
  registrations, session lookup, and the dual V1/V2 object format.

## oh-my-pi (OMP)

```sh
lsops hook install omp --mode output
lsops hook status omp
lsops hook preview omp --mode output
lsops hook uninstall omp
```

The generated extension uses ExtensionAPI session lifecycle events, not the
per-model `context` hook. It renders once at session start/resume, session
switch/branch/tree navigation, and after compaction. Each event appends one hidden
`lsops.catalogue` message to session history without requesting an extra turn.
Ordinary model calls, tool continuations, and model changes do not run lsops or
append/reposition its output. Existing history and other extensions are untouched.

The pinned lsops executable receives literal argv and the active `ctx.cwd`.
What/why/when guidance, `--limit`, and registered project overlays use the live
renderer. Edits are picked up at the next lifecycle refresh, not every model call.
Invalid directories and failed renders add no message; an earlier catalogue may
remain in history until normal compaction. In-flight results are discarded after
a newer lifecycle event or shutdown. No registered CLI is executed.

User scope honors `PI_CODING_AGENT_DIR` first, otherwise `PI_CONFIG_DIR/agent`,
otherwise `~/.omp/agent`. For a named profile, target its actual agent directory
when installing, for example:

```sh
PI_CODING_AGENT_DIR="$HOME/.omp/profiles/work/agent" lsops hook install omp --mode output
```

OMP's own `--profile work` then discovers that directory normally. Project scope
writes `.omp/extensions/lsops.js` in the selected directory; upstream discovery is
cwd-only, not an ancestor walk. Restart or reload OMP after install/uninstall.
Disabled extension discovery, disabled extensions and project trust are not
changed. Installing a hook is not proof it has loaded in an authenticated session.

Sources checked 2026-10-23:
- [OMP extension discovery](https://github.com/can1357/oh-my-pi/blob/main/docs/extension-loading.md)
- [ExtensionAPI context event](https://github.com/can1357/oh-my-pi/blob/main/docs/extensions.md)
