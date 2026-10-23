> Historical validation report supplied with the input archive. These are upstream claims, not newly rerun native-platform or authenticated-harness checks.

# Validation for lsops 0.2.0

Checked on 2026-10-23 in a Linux amd64 workspace, using Go 1.27.3.

## Automated checks

- `go test -count=1 ./...` passed for all five internal packages.
- `go vet ./...` passed.
- Registry tests cover user-written descriptions, optional literal executable
  paths, missing executables, v1 compatibility, atomic edits, field preservation,
  malformed inputs, and path validation.
- App tests prove the default list contains only configured entries, automatic
  discovery stays optional, unavailable registered commands stay visible, and
  hooks emit the current catalogue without executing a registered command.
- Hook renderer tests cover all supported transport envelopes, instructions,
  output, modes, limits, path quoting, scopes, home overrides, and live edits.
- Installer tests cover five JSON target lifecycles, repeated installs with no
  file change, mixed foreign hook groups, exact-byte backups, malformed/JSONC/
  duplicate-key refusal, incompatible schemas, symlink/nonregular destinations,
  concurrent-change detection, and Unix/Windows command encoding.
- Literal dollar signs survive Gemini settings interpolation. Paths containing
  symlinks and parent traversal keep their filesystem meaning.
- Existing optional-discovery tests cover PATH precedence, shadowing, executable
  filtering, Windows PATHEXT behavior, and passive npm/Python/man metadata.

The OpenCode subprocess tests required normal child-process output outside the
restricted execution sandbox. The complete suite passed with that capability;
no test was disabled to obtain the pass. Permission tests that require a
non-root user remain skipped when run as root.

## Built-binary end-to-end checks

The release Linux amd64 binary was installed into a temporary directory whose
name contained spaces and an apostrophe. It reports `lsops 0.2.0`.

A temporary project exercised all six harness installers. Repeating the install
left each integration file's bytes and modification time unchanged. Six actual
JSON hook commands across Claude, Gemini, Cursor, Codex, and Copilot were run
(two events for Gemini); every extracted context payload exactly matched current
plain `lsops` output.

The catalogue was changed after hook installation. Running the same commands
produced the new descriptions, removed the deleted entry, and did not rewrite
any hook file. Switching Claude to instructions mode produced a correctly quoted
binary/config instruction without the catalogue. Uninstalling all integrations
preserved a foreign Claude hook, another setting, and an unrelated OpenCode
plugin. A marker verified that no registered CLI was executed by inventory.

## OpenCode adapter

The generated JavaScript was imported and exercised with mocked V1 and V2 host
APIs. Tests cover exact argument arrays, session directory lookup/fallback,
timeouts, output bounds, generic error reporting, cleanup, and fresh execution
on consecutive requests. A separate smoke check used an actual compiled lsops
binary through the generated plugin, changed the registry between callbacks,
and observed the updated description on the second request.

These are protocol and adapter checks, not an authenticated live OpenCode session.

## Release targets

| Target | Build | Runtime verification |
| --- | --- | --- |
| Linux amd64 | Built | Go suite, binary, installer, and generated hook commands |
| Linux arm64 | Cross-compiled | Not run on target |
| macOS amd64 | Cross-compiled | Not run on target |
| macOS arm64 | Cross-compiled | Not run on target |
| Windows amd64 | Cross-compiled | Synthetic path/encoding checks only; not run on target |

All five binaries were built with CGO disabled, trimpath, embedded version 0.2.0,
and no third-party modules. Go build metadata confirms the intended OS and
architecture. The source declares Go 1.23 as its minimum; this release was built
and tested with Go 1.27.3. `SHA256SUMS` covers the bundled binaries.

## Practical limits

No authenticated live Claude, Codex, Gemini, Cursor, or Copilot session was run.
Harness APIs were checked against the primary sources linked in
[Agent integration](agents.md), and the generated commands/protocols were
validated locally. Installed configuration still requires any native trust or
reload step imposed by the harness.

Immediate context reinjection after compaction is supported by the selected
Claude/Codex events and OpenCode per-request plugin. Gemini refreshes at startup
and submitted turns. Cursor and Copilot are startup-only integrations. The tool
does not present unsupported events as working post-compaction hooks.
