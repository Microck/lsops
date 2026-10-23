<div align="center">
  <img src=".github/assets/lsops.png" width="160" alt="lsops logo">
  <h1>lsops</h1>
  <p>A CLI catalogue for coding agents.</p>
  <a href="https://github.com/Microck/lsops/actions/workflows/check.yml"><img src="https://img.shields.io/github/actions/workflow/status/Microck/lsops/check.yml?branch=main&style=flat-square&label=ci&color=000000" alt="CI status"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-000000?style=flat-square" alt="MIT license"></a>
</div>

Register the commands you want your agent to know about and write a description
for each. `lsops` lists those commands, checks whether their executables are
available, and can add the list to your agent's context through a local hook.
Listing and discovery never run registered commands. PATH discovery is optional.

`lsops` is a standalone Go binary with no third-party dependencies or background
service. The catalogue stays in a local JSON file.

## Install

Build from source with Go 1.23 or later on Linux or macOS:

```sh
git clone https://github.com/Microck/lsops.git
cd lsops
sh install.sh
export PATH="$HOME/.local/bin:$PATH"
lsops --version
```

On Windows, run `.\install.ps1` in PowerShell and use the executable path it
prints. The installers do not edit shell profiles or configure agent hooks.
Keep the installation directory on your shell's and agent's PATH for future
sessions. See the [installation guide](docs/usage.md#install) for custom paths
and release bundles with prebuilt binaries.

## Use it

```sh
lsops add gh "Work with GitHub issues and pull requests"
lsops add parallel-cli "Search the web with Parallel"
lsops
```

Run `add` again to update a description, or `lsops remove gh` to remove an entry.
Registered commands stay in the list when unavailable. Availability means lsops
found an executable, not that the command is authenticated or its service works.

For an executable outside PATH, pass a literal absolute path:

```sh
lsops add acme "Query internal analytics" --path /opt/acme/bin/acme
```

`acme` and its path are fictional examples.

```sh
lsops web                  # Filter names and descriptions
lsops --json               # Print the catalogue as JSON
lsops --paths              # Include resolved executable paths
lsops --limit 20           # Cap results and report omissions
lsops discover             # Scan PATH without running commands or registering them
lsops config               # Print the selected config path
```

Use `--config FILE` or `LSOPS_CONFIG` to select another catalogue. The
[command reference](docs/usage.md) covers configuration, output formats, and
discovery.

## Connect an agent

Hooks support Claude Code, Codex CLI, Gemini CLI, Cursor, GitHub Copilot CLI, and
OpenCode. For Claude Code:

```sh
lsops hook preview claude --mode output
lsops hook install claude --mode output
lsops hook status claude
```

Replace `claude` with `codex`, `gemini`, `cursor`, `copilot`, or `opencode` for
another agent. User scope is the default. Add `--scope project` to configure
only the current project.

`output` mode injects the current catalogue. The default `instructions` mode
tells the agent how to request it. Each hook invocation reads the catalogue
again, so edits take effect at the next supported event. Refresh timing varies
by agent, especially after compaction. See [refresh behavior and
limits](docs/agents.md#refresh-behavior).

Hook installation preserves unrelated settings and backs up files before
changing them. You still need to approve or trust hooks where the agent requires
it. To remove the integration:

```sh
lsops hook uninstall claude
```

## Development

```sh
go test -count=1 ./...
go vet ./...
go build -trimpath -buildvcs=false -o lsops .
```

The OpenCode plugin tests also need Node.js 24. See [contributing](CONTRIBUTING.md)
for the full checks, [validation](VALIDATION.md) for platform limits, and the
[release runbook](docs/release-runbook.md) for packaging.

Licensed under [MIT](LICENSE).
