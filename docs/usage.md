# lsops

Your curated CLI catalogue for AI agents.

Tell `lsops` which commands matter and how to describe them. Agents get that list
on demand or through a live harness hook. Automatic PATH discovery is optional.
Go, standard library only, standalone binaries.

## Install

Extract the release ZIP and run from its directory:

```sh
sh install.sh
export PATH="$HOME/.local/bin:$PATH"
lsops --version
```

The Unix default is `~/.local/bin/lsops`. The installer accepts
`sh install.sh --dir "$HOME/bin"` and never edits shell profiles. The `export`
above affects only the current shell; keep the chosen directory on your shell's
and agent's PATH for future sessions.

Windows PowerShell:

```powershell
.\install.ps1
& "$env:LOCALAPPDATA\lsops\bin\lsops.exe" --version
```

The Windows installer accepts `-InstallDir 'C:\Tools\lsops'` and reports whether
the directory needs adding to PATH.

Binaries are included for Linux amd64/arm64, macOS amd64/arm64, and Windows amd64.
A matching binary needs no Go installation. If the native binary is missing,
the installer can build the included source using an installed Go 1.23 or later,
with downloads disabled.

## Choose your CLIs

```sh
lsops add parallel-cli "Search the web with Parallel"
lsops add gh "Work with GitHub issues and pull requests"
lsops
```

You supply the descriptions. `lsops` does not infer descriptions in its default
view, enumerate unrelated PATH entries, or register anything automatically.

For an executable outside PATH:

```sh
lsops add acme "Query internal analytics" --path /opt/acme/bin/acme
```

`acme` and its path are fictional examples. `--path` is one literal, absolute
executable path, not a shell command or argument string. The explicit invocation
path is included in text output so an agent can actually use it.

Run `add` again to change a description. Omitting `--path` preserves an existing
explicit path. To clear that path, remove the `path` field from the config, or
remove and re-add the entry.

```sh
lsops remove gh
lsops config                 # Print the selected config path
lsops config example         # Print sample JSON without writing it
```

`describe` and `forget` remain aliases for `add` and `remove`.

Configured CLIs remain listed when they cannot be located or executed in the
current environment, with an availability annotation. PATH is used only to look
up each configured name. This lookup never executes the tool and says nothing
about authentication or service readiness. Shell functions and aliases are not
resolved.

## Everyday views

```sh
lsops                        # Your curated list
lsops web                    # Filter names or descriptions
lsops --paths                # Include resolved paths
lsops --names                # Names only
lsops --json                 # Machine-readable catalogue
lsops --context              # Add a brief agent-facing introduction
lsops --limit 20             # Explicitly limit results; report omissions
```

The default is alphabetical and unlimited. Filters are case-insensitive
substrings; multiple filters match any supplied term. `lsops list FILTER` handles
reserved subcommand names. Global flags go before filters/subcommands;
`list` and `discover` also accept flags immediately after their subcommand.

### Optional PATH discovery

```sh
lsops discover
lsops discover --known
lsops discover --all
lsops discover --json web
```

Discovery shows external executable names found on the current PATH and does
not change your curated list. Descriptions come from local npm/Python metadata,
manual pages, your config, or a bundled name-based catalog. Unknown entries stay
visible unless `--known` is used. `--all` includes a small set of common system
primitives hidden in the discovery view. These options do not hide commands
that you explicitly included in the curated list.

The equivalent global flag is `--discover`. Activate the intended virtual
environment or container before discovery. Inventory reads local files; it does
not run the discovered commands, invoke native help, or contact a service.

## One-command harness integration

After installing the binary, choose a harness and hook mode:

```sh
lsops hook install claude --mode output
lsops hook install codex --mode output
lsops hook install gemini --mode output
lsops hook install cursor --mode output
lsops hook install copilot --mode output
lsops hook install opencode --mode output
```

Or configure all six in one command:

```sh
lsops hook install all --mode output
```

`all` creates integrations for every supported target; it does not detect which
harnesses you have installed. User scope is the default. For the current project:

```sh
lsops hook install claude --scope project --mode output
```

Two modes are available:

| Mode | Agent receives |
| --- | --- |
| `instructions` (default) | A short instruction to invoke the selected lsops binary and config when it needs the catalogue. |
| `output` | The current text from `lsops`, directly included as context. No extra usage instruction is prepended. |

Every installed integration calls the binary afresh. Add/remove/edit operations,
manual config edits, and replacing the binary at the same path affect the next
hook invocation without reinstalling or regenerating a snapshot. Existing context
already sent to a model is not retroactively changed. Reinstall to change mode,
limit, config location, or binary location.

| Harness | Installed refresh behavior |
| --- | --- |
| Claude Code | Session start/resume/reset/fork and immediately after compaction via `SessionStart`. |
| Codex CLI | The same `SessionStart` sources, including compaction before the next model request. Its `/hooks` trust review is still required. |
| Gemini CLI | Session start and every submitted user turn. No immediate mid-turn post-compaction refresh is promised. |
| Cursor | Asynchronous session-start context. No documented immediate post-compaction injection. |
| GitHub Copilot CLI | Session-start context. No documented immediate post-compaction injection. |
| OpenCode | Before each model request, including continuation after compaction; V2 and V1 1.18.29+ plugin formats. |

The installer reports each target's actual coverage. It does not change a
harness's trust decisions, approval policy, or hook-disable settings. Restart or
reload the harness after installation as required by that harness. See
[Agent integration](agents.md) for paths, source documentation, and limits.

```sh
lsops hook targets
lsops hook status all
lsops hook preview claude --mode output
lsops hook uninstall claude
lsops hook render --mode output       # Inspect the exact plain-text payload
```

Install/update/remove preserve unrelated hooks and settings. Existing files get
an exact-byte backup before modification. Reinstalling the same configuration
is idempotent. Malformed or commented JSON settings are rejected unchanged rather
than rewritten. The installer does not parse JSONC.

## Configuration

Path precedence: `--config FILE`, then `LSOPS_CONFIG`, then
`$XDG_CONFIG_HOME/lsops/config.json` when set, otherwise the platform's user-config
directory plus `lsops/config.json`. Typical defaults are `~/.config` on Linux,
`~/Library/Application Support` on macOS, and `%APPDATA%` on Windows.
`lsops config` prints the actual choice.

```json
{
  "version": 1,
  "tools": {
    "parallel-cli": {
      "description": "Search the web with Parallel",
      "help": "parallel-cli --help"
    },
    "acme": {
      "description": "Query internal analytics",
      "path": "/opt/acme/bin/acme"
    }
  }
}
```

`help` is an optional display hint, never executed by lsops. `hide: true` excludes
an entry from all views. Legacy `include: true` only affects common-command
filtering during automatic discovery. `hide` and `include` cannot both be true.

Descriptions must be nonblank single lines of at most 240 Unicode characters;
help hints at most 500; paths at most 4096. Configs are limited to 1 MiB. Unknown
fields and invalid files are rejected. New files use mode `0600` and new config
directories `0700` on Unix. Edits are atomic and refuse symlink destinations.

## Upgrade from 0.1

Re-run the release installer to replace the binary at its existing path.
Existing v1 config descriptions become curated entries automatically. Legacy
entries with no description stay in the config but are omitted from the curated
view. Use `lsops discover` for the old automatic inventory behavior.

The old `lsops hook claude` command remains a preview alias. If you manually
installed a 0.1 snippet, remove that old entry before installing the managed 0.2
hook; it has no management marker and is deliberately not removed automatically.

## JSON interface

`lsops --json` emits `schema_version: 2`, `lsops_version`, `mode` (`curated` or
`discover`), `tools`, `summary`, and `warnings`.

Each tool includes `name`, `description`, `path`, `description_source`,
`registered`, `available`, `help_hint`, `help_shell`, and `system_command`.
Discovery may also include `shadowed_paths`. Empty unresolved paths are `""`.
Availability is a local executable lookup, not an authentication check. Curated
entries have `description_source: "config"`; discovery also uses `npm:PACKAGE`,
`python:DISTRIBUTION`, `man:PATH`, `builtin`, and `unknown`.

`summary.candidates` reconciles with `shown`, `hidden_system`, `hidden_configured`,
`hidden_unknown`, `missing_description`, `filtered_out`, and `omitted_by_limit`.
`configured` counts stored records; `discovered` counts automatic PATH candidates;
`unavailable` is a subset of the displayed entries. `system_command` classifies common primitives during discovery; it is false in
the curated view. Help hints are suggestions;
`help_shell` is `posix`, `powershell`, or `user-defined`.

Exit codes: `0` for success, including empty results; `1` for an operational or
configuration error; `2` for invalid usage.

## Build and verify

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  go build -trimpath -buildvcs=false -ldflags '-s -w' -o lsops .
sh scripts/build-release.sh 0.2.0
```

Go 1.23+; no third-party modules. See [Validation](../VALIDATION.md) for checks and
platform limits. `SHA256SUMS` covers the five bundled binaries.

[MIT license](../LICENSE).
