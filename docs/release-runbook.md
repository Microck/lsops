# release runbook

1. Select an unused semantic version and update `main.go`, the build script default,
   release notes and examples. Never move a published tag or overwrite artifacts.
2. Use a clean source checkout. Run formatting, tests, race detection, vet and native
   CI on Linux, macOS and Windows. Keep skipped tests visible.
3. Build from source with `sh scripts/build-release.sh VERSION`. Do not reuse the
   opaque binaries from the supplied archive. Test the matching native binary and
   install it only into a disposable directory.
4. Assemble source, docs, installers and the five `bin/OS-ARCH` binaries into a
   versioned bundle. Generate SHA256 checksums from its actual bytes. Inspect the
   archive for secrets, caches, user configuration and unexpected files.
5. Publish only when explicitly authorized. Attach the bundle and checksums together;
   state which targets were executed versus merely cross-compiled, and that no
   authenticated harness end-to-end session is implied.
6. Download the published assets again, verify checksums, inspect the tag commit and
   test a fresh install. Do not claim completion from an upload response alone.

No npm package or external publishing account is used.

## 0.4.0 publication

The version-scoped release workflow validates pull requests without publishing.
For the authorized 0.4.0 release, squash-merge the release preparation PR with the
exact title `chore(release): prepare v0.4.0`. The main push must change
RELEASE_NOTES.md. The workflow reruns native CI and bundle validation, then uses
the existing GitHub Actions token in the publish job only to create v0.4.0 with
both assets attached. No new credentials or external package account are used.
Other commit titles, pull requests and future versions cannot publish through
this version-scoped workflow.

The job checks the exact commit subject before publication, allowing a squash
commit body but rejecting title suffixes. It refuses existing releases and tags
pointing elsewhere. An orphaned lightweight tag pointing exactly to the current
commit can be reused if no release exists; it is never moved.
The job downloads the published bundle again,
checks both checksum manifests and performs a fresh disposable Linux install.
If publication partially fails, inspect the actual release and assets before any
retry. Never delete or overwrite a published version to conceal a failed run;
use a new version for corrections.
