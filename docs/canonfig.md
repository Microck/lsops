# Canonfig add-on

The optional add-on lives in **lsops**. Canonfig transports a reviewed portable
catalogue as an ordinary file; lsops reconciles it into its own local registry.
No Canonfig plugin, patch, network call, or executable invocation is required by
these commands. The bridge works with Canonfig's existing file resources.

## Source: capture a portable catalogue

Use `lsops config` to find this machine's actual base configuration. Project
catalogues are not implicitly loaded. Create the staging directory first.
Global flags must precede `canonfig`; command flags follow `capture` or `sync`.

```sh
lsops --config /home/me/.config/lsops/config.json canonfig capture --bundle ./lsops-shared.json
lsops --config /home/me/.config/lsops/config.json canonfig capture --bundle ./lsops-shared.json --apply
```

Preview is default. Capture exports visible described tools: description,
why/when, install URL, manager and reviewed script SHA256. It excludes paths,
help overrides, visibility flags, project trust and update approvals. Authored
text is not secret-scanned: review it before sharing. No credentials or installed
version inventory is discovered or exported.

Add this resource to your existing authored Canonfig machine profile. Keep the
captured file beside the profile (the source path is relative to that profile):

```json
{
  "id": "lsops-catalogue",
  "kind": "file",
  "policy": "replace-if-unmodified",
  "target": "~/.canonfig/lsops-shared.json",
  "spec": { "kind": "file", "source": "lsops-shared.json" },
  "verify": { "method": "digest" }
}
```

Use Canonfig's normal digest/review, publish, enrollment and follower sync
workflow. **Never target the follower's live lsops config**. Capture does not
publish a revision, enroll machines or change schedules. Source catalogue edits
enter the next explicit publication.

## Follower: merge shared metadata while keeping local CLIs

After Canonfig delivers the staging file:

```sh
lsops --config /home/me/.config/lsops/config.json canonfig sync --bundle /home/me/.canonfig/lsops-shared.json
lsops --config /home/me/.config/lsops/config.json canonfig sync --bundle /home/me/.canonfig/lsops-shared.json --apply
```

Use the actual config path on macOS/Windows. Both parent directories must exist.
A missing live config starts empty; a missing staging file is an error, never an
instruction to delete tools. Paths resolve existing parent aliases, but leaf
symlinks, nonregular files, reserved paths and aliases of local state are refused.

- Follower-only tools and local project trust stay untouched.
- New shared names are added and recorded as bridge-owned.
- An identical pre-existing entry stays locally owned, so later source removal
  cannot delete it.
- An independently registered same-name tool with different metadata blocks the
  entire batch. Case collisions also block on every OS.
- Source updates replace only unchanged bridge-owned portable fields. Local
  executable paths, help and visibility settings survive.
- Source removal clears only unchanged bridge-owned metadata. Local-only fields
  remain; otherwise the catalogue record is removed. **No executable is ever
  uninstalled.** A changed or locally deleted owned entry is a conflict.

Install and update remain separate lsops operations. Preview `lsops install
--all`, then explicitly authorize installation if appropriate. Existing automatic
update approvals are never imported or rewritten; changing an install identity
causes the existing updater's exact-source check to require a new approval.

## Automatic local reconciliation

```sh
lsops canonfig capture --bundle ./lsops-shared.json --apply --watch --interval 30s
lsops canonfig sync --bundle /home/me/.canonfig/lsops-shared.json --apply --watch --interval 30s
```

Watch is opt-in and foreground, with a 30-second default (1 second to 24 hours).
It stops on conflicts/errors, does no idle writes and exits on Ctrl+C/SIGTERM
between completed passes. It does not install an OS service. A single `--apply`
pass can instead run under your existing scheduler. Add global `--json` before
`canonfig` for one JSON result per pass. Follower polling does not fetch a new
revision; keep Canonfig's separate synchronization schedule.

This supports lsops-to-shared capture and shared-to-lsops reconciliation, **not
multi-master conflict resolution**. Each published catalogue has one source of
truth. To promote a follower-only tool, deliberately add it on the source or
review a separate capture before incorporating it. Do not run capture and sync
against the same shared file on competing machines.

## Concurrency and interrupted writes

Stop other catalogue writers (add/remove/import, project edits, other capture
processes) before applying. Sync shares the install/update lock and adds a bridge
lock, but this is not an atomic transaction with arbitrary editors or Canonfig.
Inputs are rechecked before writing; that detects changes, not a cross-process
compare-and-swap guarantee. A staging refresh can be picked up on the next pass.

Ownership is stored next to the live config as `.canonfig.json` (suffix appended
to the config filename). Before mutation, `.canonfig.json.pending` records the
prior live config and prior ownership receipt, plus planned outputs. Both files
use flushed temporary-file replacements. Success retains the journal as
`.canonfig.json.last-transaction.json`. These private files can contain local
paths and authored text: do not publish or commit them.

After interruption, further sync fails closed. Stop writers, inspect the pending
journal, and restore **both** `before` and `state_before` snapshots (a null means
that file did not exist). Retain a private backup first. Only then remove the
pending journal and stale locks after verifying no worker is active, and preview
again. Never clear a lock just because a process seems slow. No automatic crash
recovery or forced takeover is provided. An interrupted capture leaves its
capture lock for operator inspection as well.

POSIX writes flush files and parent directories. Windows flushes file data and
uses rename plus the retained receipt; directory-entry power-loss durability is
not claimed. Sudden termination can require the explicit recovery above.
