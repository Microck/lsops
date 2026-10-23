# Unreleased

- Add oh-my-pi (OMP) live context integration with user/project/profile-directory
  installation, current what/why/when output and project-aware refresh.
- Preserve other messages, remove stale owned context on failure, and retain
  managed-file ownership, backup and symlink protections.

# lsops 0.3.0

- Optional why/when guidance complements each tool's existing description (what).
- Add or update guidance with --why and --when; omitted flags preserve it and
  explicit empty values clear it.
- Guidance appears in text, JSON and automatic output-mode hooks, and is searchable.
- Existing description-only configs remain supported. Older lsops versions reject
  configs containing the new fields; remove why/when before downgrading.
- README explains automatic output installation and event-driven catalogue refresh.

- Project add/remove/list explicitly registers trusted roots for .lsops.json overlays.
- The nearest registered project is selected from the hook/CLI working directory.
  Unregistered repository files are ignored; invalid selected files fail closed.
  See docs/projects.md for selection, trust and full-record replacement semantics.
- Older binaries also reject the projects config field; unregister projects before
  downgrading.

## installation and verification

Download lsops-0.3.0.zip and SHA256SUMS.txt together and verify the zip checksum.
The bundle contains source, documentation, installers, five rebuilt binaries and
an internal SHA256SUMS file covering those binaries. Extract, then run install.sh
or install.ps1 as documented in README.md.

Native CI tests source on Linux amd64, macOS arm64 and Windows amd64, including
race checks, vet, compilation and version smoke. Release packaging cross-compiles
all five binaries and executes the packaged Linux amd64 binary and a disposable
installation. The other packaged targets are not independently executed by the
packaging job. No authenticated harness end-to-end sessions are claimed.
