# portable install links and CLI updates

lsops can attach a supported package or explicitly trusted installer link to each registered CLI, export a
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
| `https://www.npmjs.com/package/NAME` (including scoped names) | npm + Node.js (default) | `npm install --global -- NAME@latest` |
| same npm package URL + `--manager bun` | Bun | `bun add --global -- NAME@latest` |
| same npm package URL + `--manager pnpm` | pnpm | `pnpm add --global -- NAME@npm:NAME@latest` |
| `https://pypi.org/project/NAME` | uv (default) | `uv tool install --upgrade -- NAME` |
| same PyPI URL + `--manager pipx` | pipx | `pipx install -- NAME` / `pipx upgrade -- NAME` |
| `https://crates.io/crates/NAME` | Cargo | `cargo install --locked -- NAME` |
| `https://formulae.brew.sh/cask/NAME` | Homebrew (macOS) | `brew install --cask -- NAME` / `brew upgrade --cask -- NAME` |
| `https://formulae.brew.sh/formula/NAME` | Homebrew | `brew install --formula -- NAME` / `brew upgrade --formula -- NAME` |
| `https://pkg.go.dev/MODULE/cmd/COMMAND` | Go | `go install MODULE/cmd/COMMAND@latest` |

Custom Homebrew taps are supported through concrete formula/cask file links:
`https://github.com/OWNER/homebrew-TAP/blob/main/Formula/NAME.rb` (or `Casks/NAME.rb`),
including equivalent raw.githubusercontent.com links. They map to the fully
qualified `OWNER/TAP/NAME`, and Homebrew may tap that repository. The branch
component identifies a page, **not a revision pin**: Homebrew resolves its current
tap metadata. Plain repository links and nested formula paths are not inferred.

Choose alternate managers explicitly, for example:

```sh
lsops source example https://www.npmjs.com/package/example --manager bun
# Or configure it atomically when adding a tool:
lsops add example "Example CLI" --install-url https://www.npmjs.com/package/example --install-manager bun
```

`example` is illustrative. Changing from npm to bun is not a migration: the
other manager's prior install remains, and PATH precedence is the operator's
responsibility. A manager choice must match the URL's ecosystem; there is no
fallback that silently changes managers. pnpm uses an explicit npm alias to
avoid interpreting package names as its special runtime/toolchain selectors. Bun/pnpm apply their own package-script
trust policies; lsops does not approve blocked lifecycle scripts for them.

Use a Go **command** package, not a library, and a Cargo crate with binaries. Managers must already be installed
and available on PATH; lsops does not bootstrap them, use sudo, or accept their
interactive prompts. npm, bun, pnpm, uv, pipx, Cargo and Go work on their supported Linux/macOS/Windows
platforms; Homebrew is Linux/macOS only. Windows npm/pnpm use Node.js and the manager CLI JavaScript beside their
standard launchers, not `cmd.exe` or a shell command string. Nonstandard pnpm
shims/layouts may need a normal Node.js installation layout.

HTTPS installer scripts are supported only through the explicit hash-pinned
workflow below, including raw GitHub script URLs. Archive/binary assets, private
registry URLs and arbitrary pasted shell pipelines are not package sources.
Unknown package hosts are not guessed. Such tools can remain normal catalogue
entries or use a publisher's reviewed, noninteractive installer script. Package managers use their own
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
the package identity and publisher. `latest` follows the selected manager: publisher-controlled npm registry
`latest` tags (npm/bun/pnpm), compatible Python packages (uv/pipx), Cargo registry
resolution, Go `@latest`, or current Homebrew formula/cask metadata. This is not a guarantee of the numerically newest version:
platform/runtime compatibility, a versioned formula, Homebrew pins, registry
configuration and manager policy can limit it. npm/bun/pnpm/Go/uv/Cargo operations deliberately
request a fresh latest version rather than preserving an old manually installed
pin. To preserve such a tool, exclude it from the selection and automatic
approvals. Homebrew pins are not overridden; pipx upgrade retains its existing environment
settings and constraints. Neither package-manager policy nor blocked lifecycle
scripts are bypassed.

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

Approvals cover the exact URL, manager selection and script digest. They live
beside the selected catalogue at `CONFIG.updates.json`. Keep this
file local; exporting a catalogue never includes it. Changing/removing a URL, manager or script digest, or
hiding/deleting its tool suspends that approval until the exact approved source is
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


## curl / install.sh and PowerShell installer links

Use the **HTTPS URL from the curl command**, not the entire `curl | sh` pipeline.
lsops downloads the script itself; curl is not required. No URL extension
implicitly authorizes an interpreter. Download to a new local file for review:

```sh
lsops script fetch https://example.com/install.sh installer.sh
```

This does not run it. It writes a new private file and prints SHA-256. Inspect the
whole script and verify the publisher/source independently before using that
digest; computing a hash is not a security review. Then configure and preview:

```sh
lsops add example "Example CLI"
lsops source example https://example.com/install.sh --manager sh --sha256 REVIEWED_SHA256
lsops install example
lsops install --yes --allow-scripts example
lsops auto-update enable --yes --allow-scripts example
```

The URL and `REVIEWED_SHA256` are placeholders. The digest must be exactly 64
lowercase hexadecimal characters. `--install-manager` and `--install-sha256`
can also be passed to `add` atomically with `--install-url`. Use `bash` instead
of `sh` for a Bash script, or `pwsh` / `powershell` for a PowerShell script.
The interpreter must already exist on PATH. PowerShell is invoked with
`-NoLogo -NoProfile -NonInteractive -File`; execution policy is not bypassed.
Installers must work without additional script arguments or interactive input.
A script may itself bootstrap a manager, but lsops does not infer dependency
order between tools: install prerequisites explicitly before a bulk operation.

Every execution downloads the **complete** script over verified HTTPS, checks
the approved digest, writes it privately, and only then invokes the selected
interpreter with that file. It never streams network bytes into a shell and
never evaluates the URL as shell text. The size cap is 4 MiB, the download
request has a 30-second timeout, and redirects are bounded and must remain HTTPS
without credentials, query or fragment. This intentionally excludes signed or
query-bearing installer links; use a stable public publisher URL instead.

A changed script fails closed before execution. Review the new contents, update
the SHA-256 pin, and explicitly re-enable its automatic update approval. lsops
never auto-refreshes pins. Re-running an unchanged reviewed installer can fetch
its publisher's latest CLI if that is what the script implements; lsops cannot
infer version semantics or guarantee idempotency for arbitrary installers.
The pin protects the entry script, **not** everything it may download or execute.
Approved scripts can modify your account/system with your user's privileges;
they are not sandboxed. Separate `--allow-scripts` is required even with `--yes`
(and for `auto-update enable`), so importing a bundle cannot silently opt into
script execution. A mixed bulk selection lacking this consent fails before any
tool runs. Auto-update worker invocations use the previously recorded approval.

### command references

The adapters follow the managers' own install/update semantics:
[npm global installs](https://docs.npmjs.com/downloading-and-installing-packages-globally/),
[Bun add](https://bun.sh/docs/pm/cli/add),
[pnpm add](https://pnpm.io/cli/add),
[uv tools](https://docs.astral.sh/uv/concepts/tools/),
[pipx app upgrades](https://pipx.pypa.io/stable/how-to/manage-installed-apps.html),
[Cargo install](https://doc.rust-lang.org/cargo/commands/cargo-install.html), and
[Homebrew commands](https://docs.brew.sh/Manpage).
