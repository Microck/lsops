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

No npm package, external publishing account or automatic release has been created.
