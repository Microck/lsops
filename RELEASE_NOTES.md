# lsops 0.5.0

- Add supported per-CLI package install links, explicit preview-first install and
  latest-version update commands, and portable catalogue export/import.
- Add separate exact-link automatic-update approvals, a foreground periodic
  worker, and one-shot runs for user-managed OS schedulers. No service is installed
  implicitly; catalogue listing and agent hooks remain non-executing.
- Support npm/bun/pnpm, uv/pipx, Cargo, Homebrew formulae/casks and Go package
  pages, plus explicitly approved SHA-256-pinned HTTPS installer scripts. See
  docs/install-updates.md for requirements, trust boundaries and platform limits.

## 0.5.0 verification and limits

- Source tests, race detection, vet and formatting pass; native GitHub CI runs
  on Linux, macOS and Windows. Platform-specific fixture skips remain visible.
- Five bundled targets are built from source: Linux amd64/arm64, macOS
  amd64/arm64 and Windows amd64. Packaging executes the bundled Linux amd64
  binary and a fresh disposable installation; the other bundled executables
  are cross-compiled, not independently run by the publishing job.
- Installer tests use locally authored fixtures. No live third-party installer,
  authenticated harness session or native scheduler setup is implied.
- Automatic updates are opt-in. Package managers/interpreters must already be
  installed; scripts require an explicit runner, reviewed SHA-256 pin and
  separate `--allow-scripts` approval. Changed script bytes fail closed.
- See docs/install-updates.md for supported links, portable setup bundles,
  manager-specific behavior and limits. Updating lsops alone does not install
  tools or create a background service.

Download lsops-0.5.0.zip and SHA256SUMS.txt together, verify the archive checksum,
extract, then run install.sh or install.ps1. The archive contains source, docs,
installers, five rebuilt binaries and an internal SHA256SUMS manifest.

# lsops 0.4.1

- Fix OMP injection timing: session start/resume, navigation and post-compaction
  only. No subprocess or catalogue injection on ordinary model/tool turns.
- Reinstall with `lsops hook install omp --mode output`, then restart/reload OMP;
  updating the binary alone does not replace an already installed extension.

# lsops 0.4.0

- Add oh-my-pi (OMP) live context integration: `lsops hook install omp --mode output`.
- Refresh what/why/when and explicitly registered project catalogues before every
  model request, using the active OMP session directory.
- Support user/project installation and custom or named-profile agent directories.
- Replace only lsops-owned outgoing context, remove stale content after errors,
  and preserve original messages, unrelated extensions, settings and trust.
- Existing six harness integrations and catalogue formats remain supported.

## installation and verification

Download lsops-0.4.0.zip and SHA256SUMS.txt together and verify the zip checksum.
The bundle contains source, documentation, installers, five rebuilt binaries and
an internal SHA256SUMS file. Extract, then run install.sh or install.ps1 as in README.
Reinstall the desired hook from the updated binary, then restart or reload OMP.

Native CI executes source tests, race detection, vet and version smoke on Linux
amd64, macOS arm64 and Windows amd64. Packaging cross-compiles all five targets
and executes the packaged Linux amd64 binary plus a fresh disposable install.
The other packaged binaries are not independently executed by the packaging job.
The generated OMP extension was exercised with a real lsops subprocess, including
live catalogue edits and project selection. No authenticated OMP model session
or automatic harness-trust approval is claimed.
