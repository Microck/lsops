# security

Do not post credentials, private catalogues or real harness settings in issues.
Report a suspected vulnerability privately through the repository owner's verified
GitHub contact route. This repository does not promise a configured private
advisory channel or response SLA.

Catalogue descriptions and discovered metadata are untrusted context, not agent
authority. lsops does not execute registered CLIs during inventory. Installing a
hook intentionally changes local harness configuration; preview first and retain
backups. Do not use untrusted project hooks or shared writable binary paths.

Install links are package identity metadata, not execution authority. Only the
explicit install/update commands invoke package managers, with argv (never an
evaluated shell command string). Explicitly approved script sources invoke a
selected interpreter only after the whole HTTPS download matches its pinned
SHA-256. Scripts additionally require `--allow-scripts`; changed pins need new
automatic-update approval. Pins do not validate a publisher or sandbox transitive
downloads, and are never automatically refreshed. Preview first: approved managers may execute package lifecycle/build
scripts and use user/global registry configuration. Keep PATH, managers,
catalogues and local approval files under your control. Latest releases are not
necessarily safe or backward-compatible. lsops does not inspect package code,
provide rollback, isolate package scripts, or override package-manager pins.

Portable imports exclude executable paths and project trust. Auto-update uses
separate exact-source (URL, manager, digest) approvals; changing a link does not authorize its replacement.
Do not import another person's approval file or run a shared writable catalogue
under a privileged scheduler. Project overlays never drive installation/update
operations. OS scheduler setup remains an explicit operator action.
