<div align="center">
  <img src=".github/assets/lsops.png" width="160" alt="lsops logo">
  <h1>lsops</h1>
  <p>a cli catalogue for coding agents.</p>
  <a href="https://github.com/Microck/lsops/actions/workflows/check.yml"><img src="https://img.shields.io/github/actions/workflow/status/Microck/lsops/check.yml?branch=main&style=flat-square&label=ci&color=000000" alt="ci status"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-000000?style=flat-square" alt="mit license"></a>
</div>

register the commands you want your agent to know about and write a description
for each. `lsops` lists those commands, checks whether their executables are
available, and can add the list to your agent's context through a local hook.
listing and discovery never run registered commands. PATH discovery is optional.

`lsops` is a standalone go binary with no third-party dependencies or background
service. the catalogue stays in a local json file.

## install

build from source with go 1.23 or later on linux or macos:

```sh
git clone https://github.com/Microck/lsops.git
cd lsops
sh install.sh
export PATH="$HOME/.local/bin:$PATH"
lsops --version
```

on windows, run `.\install.ps1` in powershell and use the executable path it
prints. the installers do not edit shell profiles or configure agent hooks.
keep the installation directory on your shell's and agent's PATH for future
sessions. see the [installation guide](docs/usage.md#install) for custom paths
and release bundles with prebuilt binaries.

## use it

```sh
lsops add gh "work with github issues and pull requests"
lsops add parallel-cli "search the web with parallel"
lsops
```

run `add` again to update a description, or `lsops remove gh` to remove an entry.
registered commands stay in the list when unavailable. availability means lsops
found an executable, not that the command is authenticated or its service works.

for an executable outside PATH, pass a literal absolute path:

```sh
lsops add acme "query internal analytics" --path /opt/acme/bin/acme
```

`acme` and its path are fictional examples.

```sh
lsops web                  # filter names and descriptions
lsops --json               # print the catalogue as json
lsops --paths              # include resolved executable paths
lsops --limit 20           # cap results and report omissions
lsops discover             # scan PATH without running commands or registering them
lsops config               # print the selected config path
```

use `--config FILE` or `LSOPS_CONFIG` to select another catalogue. the
[command reference](docs/usage.md) covers configuration, output formats, and
discovery.

## connect an agent

hooks support claude code, codex cli, gemini cli, cursor, github copilot cli, and
opencode. for claude code:

```sh
lsops hook preview claude --mode output
lsops hook install claude --mode output
lsops hook status claude
```

replace `claude` with `codex`, `gemini`, `cursor`, `copilot`, or `opencode` for
another agent. user scope is the default. add `--scope project` to configure
only the current project.

`output` mode injects the current catalogue. the default `instructions` mode
tells the agent how to request it. each hook invocation reads the catalogue
again, so edits take effect at the next supported event. refresh timing varies
by agent, especially after compaction. see [refresh behavior and
limits](docs/agents.md#refresh-behavior).

hook installation preserves unrelated settings and backs up files before
changing them. you still need to approve or trust hooks where the agent requires
it. to remove the integration:

```sh
lsops hook uninstall claude
```

## development

```sh
go test -count=1 ./...
go vet ./...
go build -trimpath -buildvcs=false -o lsops .
```

the opencode plugin tests also need node.js 24. see [contributing](CONTRIBUTING.md)
for the full checks, [validation](VALIDATION.md) for platform limits, and the
[release runbook](docs/release-runbook.md) for packaging.

licensed under [mit](LICENSE).

## what, why and when

the description explains **what** a tool does. optionally add **why** it is useful
and **when** the agent should choose it:

```sh
lsops add gh "Work with GitHub issues and pull requests" \
  --why "Use structured GitHub data instead of scraping pages" \
  --when "Inspecting issues, reviewing PRs or checking CI"
```

guidance appears in the normal list, JSON and output-mode hooks. each field is
limited to 240 characters; keep it concrete and include important limits.
omitted flags preserve existing guidance; `--why=` or `--when=` clears it.
existing description-only configs still work. guidance is user-authored context,
not permission to bypass an agent's trust or approval rules.
