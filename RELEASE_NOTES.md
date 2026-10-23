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
