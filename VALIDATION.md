# validation

Repository preparation checked on 2026-10-22 UTC in a Linux amd64 workspace,
using the official Go 1.27.3 toolchain with its archive checksum verified.

## rerun here

- All five internal packages passed `go test -count=1 ./...`.
- All five passed `go test -race ./...`; `go vet ./...` passed.
- Rebuilt Linux amd64/arm64, macOS amd64/arm64 and Windows amd64 binaries from source.
- The Linux amd64 binary reported `lsops 0.2.0`.
- In a temporary home and project, all six integrations installed and uninstalled.
  Reinstallation preserved bytes and modification times. Catalogue edits changed
  rendered output without rewriting hook files. Inventory did not execute the
  registered fixture CLI. Paths included spaces and an apostrophe.
- Original license attribution is preserved. The source repo excludes binaries
  supplied in the input archive and local build output.

## not claimed

Native macOS/Windows/ARM64 execution and authenticated harness sessions were not
run here. Cross-compilation is not runtime verification. CI is prepared for native
Linux, macOS and Windows but is not claimed to have run before repository creation.
The Go 1.23 minimum comes from the supplied source; this pass used Go 1.27.3.
Historical upstream evidence is kept separately in
[the supplied report](docs/validation-upstream.md).


## OMP integration

- Current upstream ExtensionAPI and native `.omp` discovery paths were inspected.
- Generated-extension fixtures cover lifecycle-only injection, zero per-model/tool
  refreshes, independent session cwd, failure redaction, invalid cwd, and cancellation
  of stale asynchronous results after navigation/shutdown.
- Temporary user/project/custom-profile installs cover idempotence, backups,
  status, foreign files and symlink refusal.
- A real built lsops subprocess verified what/why/when, project overlays, a live
  catalogue edit, sibling-directory isolation, and uninstall in a disposable HOME.
- No authenticated OMP model session or harness trust approval was performed.
