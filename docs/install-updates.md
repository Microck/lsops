# portable install links and CLI updates

lsops can attach a supported package link to each registered CLI, export a
portable catalogue, install it on another machine, and periodically update
explicitly approved tools. Listing, discovery and agent hooks still never run
package managers or contact a network. There is no implicit background service.

## establish an install link

```sh
lsops add gh "Work with GitHub" --install-url https://formulae.brew.sh/formula/gh
lsops add ruff "Lint Python" --install-url https://pypi.org/project/ruff
# add or change the link for an already registered tool:
lsops source ruff https://pypi.org/project/ruff
# clear it:
lsops source ruff -
```

The link is stored as `install_url` and appears in JSON inventory and bundle
output. Adding or changing it never installs anything or enables automatic
execution. Re-running `add` without `--install-url` preserves the link;
`--install-url=` clears it. The executable name can differ from the package name;
you explicitly choose both, and lsops does not guess a package from PATH.

Supported links and operations:

| Package page | Required manager | Install / latest update |
| --- | --- | --- |
| `https://www.npmjs.com/package/NAME` (including scoped names) | npm + Node.js | `npm install --global -- NAME@latest` |
| `https://pypi.org/project/NAME` | uv | `uv tool install --upgrade -- NAME` |
| `https://formulae.brew.sh/formula/NAME` | Homebrew | `brew install --formula -- NAME` / `brew upgrade --formula -- NAME` |
| `https://pkg.go.dev/MODULE/cmd/COMMAND` | Go | `go install MODULE/cmd/COMMAND@latest` |

Use a Go **command** package, not a library. Managers must already be installed
and available on PATH; lsops does not bootstrap them, use sudo, or accept their
interactive prompts. npm, uv and Go work on their supported Linux/macOS/Windows
platforms; Homebrew is Linux/macOS only. Windows npm uses Node.js and the npm CLI
JavaScript beside the launcher, not `cmd.exe` or PowerShell.

Arbitrary shell installers, GitHub binary assets, casks, private-registry URLs,
versioned URLs and unknown hosts are not executed. Such tools can remain normal
catalogue entries without an install link. Package managers use their own
user/global registry configuration, credentials, integrity checks and install
locations. Registry settings therefore matter even though the source link is a
public package page. No package identity, availability or trust is inferred from
the link alone.

## install and update

```sh
lsops install ruff                 # preview argv; no execution or network
lsops install --yes ruff           # explicit installation approval
lsops install --all                # preview all visible linked base tools
lsops install --all --yes
lsops update --all                 # preview latest-version operations
lsops update --all --yes
```

Options go **before names**, and global `--config FILE` goes before the
subcommand. Names or `--all` are required. The complete selection is validated
before any process runs. `--all` skips entries without links and hidden entries.
Only the selected **base** catalogue is used, never a project overlay. All
operations are passed as argv, not shell strings, and run from a temporary
working directory rather than the current project.

`--yes` approves package install/build scripts as well as installation. Review
the package identity and publisher. `latest` means npm's publisher-controlled
`latest` tag, uv's latest compatible package, Go's `@latest`, or the current
Homebrew formula. This is not a guarantee of the numerically newest version:
platform/runtime compatibility, a versioned formula, Homebrew pins, registry
configuration and manager policy can limit it. npm/Go/uv operations deliberately
request a fresh latest version rather than preserving an old manually installed
pin. To preserve such a tool, exclude it from the selection and automatic
approvals. Homebrew pins are not overridden.

Updates may include breaking major releases. lsops does not provide rollback,
verify authentication, or claim the resulting executable is on PATH. A failed
manager is reported, other selected tools are attempted, and the command exits
nonzero. There is no immediate retry. Each manager invocation has a 15-minute
timeout and closed stdin; inspect manager output for partial changes. Manager
child processes and side effects are not a transactional operation.

## share a setup link and use another machine

Export only portable metadata, not local executable paths or project trust:

```sh
lsops bundle export > tools.json
```

Host that JSON at an HTTPS URL you control (for example a raw file in your own
repository). Publishing is explicit; lsops does not upload your catalogue.
Descriptions may contain private information, so review before sharing.
This URL is your reusable setup link, not an executable script.

On another machine, install lsops using its [installation guide](usage.md#install)
and install the package managers required by your tools. Use a version containing
this feature on both machines: older lsops binaries reject the new `install_url`
field rather than silently ignoring it. Then:

```sh
lsops bundle import https://example.com/tools.json  # inspect remotely, no edits
curl --fail --location --proto '=https' --proto-redir '=https' \
  https://example.com/tools.json --output tools.json
lsops bundle import tools.json                    # review downloaded snapshot
lsops bundle import --yes tools.json              # merge that same snapshot
lsops install --all                               # review commands
lsops install --all --yes
```

The URL above is an example, not a hosted service. PowerShell users can download
with `Invoke-WebRequest -Uri https://example.com/tools.json -OutFile tools.json`
and then run the same lsops commands. Imports replace matching descriptions,
guidance and install links, preserve existing local paths/visibility, and keep
unrelated tools. They never install or approve unattended updates. Remote URLs
are preview-only: the write step requires a local file so approval is attached
to reviewable content, not a second fetch of a mutable link. Review the same file
before applying it. Imports reject local paths, help commands and project trust;
all input is strictly validated and limited to 1 MiB. HTTPS requests have a
30-second timeout and cannot redirect to HTTP or credential-bearing URLs.

## automatic latest-version updates

Automatic updates use a **separate local approval snapshot**, not a field that
an imported catalogue or project overlay can enable:

```sh
lsops auto-update enable ruff                     # preview
lsops auto-update enable --yes ruff               # approve exact link
lsops auto-update status
lsops auto-update run                             # immediate pass, then every 24h
# or:
lsops auto-update run --interval 12h
lsops auto-update run --once                      # suitable for an OS scheduler
lsops auto-update disable ruff
lsops auto-update disable --all
```

`enable --all --yes` approves every currently visible linked base tool, not
future additions. Enable does **not** start the worker or register a login task.
`run` is a foreground worker: keep it running under your own process supervisor,
or schedule `run --once` with cron, systemd, launchd or Windows Task Scheduler.
Use an absolute lsops executable path, an absolute `--config` path, your normal
(non-root) account, and a PATH containing your managers. For example, a cron
entry you explicitly install could be:

```cron
0 9 * * * /home/you/.local/bin/lsops --config /home/you/.config/lsops/config.json auto-update run --once >> /home/you/.local/state/lsops-updates.log 2>&1
```

Create the log directory first; substitute real paths and configure the
scheduler's PATH. lsops does not create this entry or change shell profiles.
On Windows, schedule the absolute `lsops.exe` path with arguments
`--config C:\Users\you\lsops\config.json auto-update run --once`, running as
that user. Native scheduler installation is intentionally outside this feature.

Approvals live beside the selected catalogue at `CONFIG.updates.json`. Keep this
file local; exporting a catalogue never includes it. Changing/removing a link or
hiding/deleting its tool suspends that approval until the exact approved link is
restored or explicitly approved again. Workers reread approvals at each pass;
new imports cannot broaden them. Disable prevents future passes, not a package
operation already in progress. Stop the foreground worker with Ctrl+C. A worker
with no eligible approvals exits. Status reports approvals, **not** whether a
service is alive. With a supervisor, disable its restart policy when stopping.

Passes do not overlap within a worker; the interval is measured after a pass
finishes, with a 1-hour minimum. Each selected catalogue has a lock directory
(`CONFIG.update-lock`) preventing overlapping lsops operations for that config.
It does not coordinate other package managers, other config files or aliases of
the same config. A killed process may leave a stale lock: inspect running
processes before manually removing it. Do not remove a live lock. Failures are
logged; a foreground worker retries at the next scheduled interval. A one-shot
pass returns nonzero on failure so the scheduler can surface it.
