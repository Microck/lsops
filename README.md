<div align="center">
  <img src=".github/assets/lsops.svg" width="160" alt="lsops logo">
  <h1>lsops</h1>
  <p>
    <a href="https://github.com/Microck/lsops/actions/workflows/check.yml"><img src="https://img.shields.io/github/actions/workflow/status/Microck/lsops/check.yml?branch=main&style=flat-square&label=ci&color=000000" alt="ci badge"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-000000?style=flat-square" alt="license badge"></a>
    <img src="https://img.shields.io/badge/dependencies-zero-000000?style=flat-square" alt="zero dependencies">
  </p>
</div>

---

`lsops` is a curated cli catalogue for ai agents. choose the commands that matter,
write their descriptions, and give your agent a live list without turning your
entire PATH into context. one binary. go standard library only. no service.

[usage](docs/usage.md) | [agent integration](docs/agents.md) | [contributing](CONTRIBUTING.md) | [release runbook](docs/release-runbook.md) | [validation](VALIDATION.md)

## why

- explicit by default: only tools you register appear in the catalogue.
- your descriptions: no guessed capabilities or automatic registration.
- live context: supported harness hooks read the current list on each event.
- passive inventory: listing and optional discovery never execute registered tools.
- local and portable: no account, network service or third-party go modules.

## install

from a source checkout, with go 1.23 or newer installed:

```sh
git clone https://github.com/Microck/lsops.git
cd lsops
sh install.sh
export PATH="$HOME/.local/bin:$PATH"
lsops --version
```

on windows, run `./install.ps1` from PowerShell and use the printed executable
path. installers do not edit shell profiles or install harness hooks. source
checkouts build locally with downloads disabled; release bundles can include
prebuilt binaries. see the [full installation guide](docs/usage.md#install).

## quickstart

```sh
lsops add gh "Work with GitHub issues and pull requests"
lsops add parallel-cli "Search the web with Parallel"
lsops
lsops --json
```

a command outside PATH can use one literal absolute executable path:

```sh
lsops add acme "Query internal analytics" --path /opt/acme/bin/acme
```

`acme` is a fictional example. availability means an executable was found locally,
not that it is authenticated or that its service works.

## agent hooks

```sh
lsops hook preview claude --mode output
lsops hook install claude --mode output
lsops hook status claude
lsops hook uninstall claude
```

supports claude, codex, gemini, cursor, copilot and opencode. use `--scope project`
for project-local configuration; user scope is the default. installation modifies
only managed entries, preserves unrelated settings and backs up existing files.

`instructions` mode tells the agent how to request the list. `output` mode injects
the list itself. neither mode changes native trust or approval requirements.
refresh timing differs by harness; not every integration refreshes immediately
after compaction. see [coverage and limits](docs/agents.md#refresh-behavior).

## more control

```sh
lsops web                  # filter the curated list
lsops --paths              # show resolved paths
lsops --limit 20           # cap results and report omissions
lsops discover             # optional passive PATH inventory
lsops config               # show the selected config path
```

configuration stays local. `--config`, `LSOPS_CONFIG` and platform defaults select
the file. malformed configuration and unsafe destination paths fail closed.
[full reference](docs/usage.md) covers JSON output, limits, discovery and upgrades.

## development

```sh
go test -count=1 ./...
go vet ./...
go build -trimpath -buildvcs=false -o lsops .
```

node.js is needed for the generated OpenCode plugin tests. no live authenticated
harness session is implied by these checks. see [contributing](CONTRIBUTING.md).

[MIT license](LICENSE).
