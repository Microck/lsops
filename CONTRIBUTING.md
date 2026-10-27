# contributing

Keep lsops small, explicit and local. Curated listing must not discover unrelated
tools, execute registered commands, contact services, or rewrite harness trust.

Use Go 1.23+ and Node.js 24 for plugin subprocess fixtures. There are no external
Go modules. Run `gofmt`, `go test -count=1 ./...`, `go test -race ./...`, `go vet
./...`, and `sh scripts/build-release.sh 0.5.0`. Report skips and platform limits.

Test settings changes only in temporary homes/projects. Never use a contributor's
real harness config, credentials or catalogue. Cover repeated install/uninstall,
foreign entries, malformed files, symlinks, quoting and catalogue refresh. Native
hook trust and authenticated model sessions require separate operator validation.

Open a focused PR with the behavior, regression test and verification results.
Keep unsupported guarantees out of the README. Preserve the MIT attribution.
