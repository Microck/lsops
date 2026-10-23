# project-specific catalogues

lsops can show a different catalogue for each explicitly enabled project.
the selected base config remains global; a project's .lsops.json overlays it only
when lsops runs inside that registered directory or its descendants.

## set up

from your project directory, first create a local catalogue, then opt the project
in using the normal base config:

```sh
lsops --config .lsops.json add gh "Work with this project's GitHub repository" --why "Use structured issue and PR data" --when "Reviewing this project"
lsops project add .
lsops
lsops project list
```

the first command edits only .lsops.json. the second writes the canonical absolute
project root into your selected base config's projects list. inspect local content
before enabling it: future edits to that file become agent-visible automatically.
use --config FILE before project add/list/remove if your hook uses a custom base
catalogue. registrations belong to that catalogue, not a machine-wide service.

## selection and overrides

- no registration means no project file is read, even if .lsops.json exists.
- the nearest registered physical ancestor wins. nested registered projects use
  their own overlay, not a chain of parent overlays.
- global entries remain unless the local file has the same tool name. a local
  record replaces that entire tool record, including why/when, path and visibility.
- to hide a global tool locally, use a record such as "gh": {"hide": true} in the
  local file's tools object.
- unregistered nested .lsops.json files are ignored. sibling directories do not
  match a project's name prefix.
- paths are resolved physically. moving a project needs a new registration.
  symlinked local catalogue files and recursive project registrations are rejected.
- a registered project's missing or malformed catalogue fails closed; lsops does
  not silently revert to a different list.
- JSON output includes project_config when an overlay was selected.

a minimal local file looks like:

```json
{
  "version": 1,
  "tools": {
    "gh": { "hide": true },
    "project-cli": {
      "description": "Inspect this project's build",
      "why": "Uses the project-specific build settings",
      "when": "Diagnosing this project's build failures"
    }
  }
}
```

these names are examples. listing never runs them. adding or editing normal
entries still writes only the explicitly selected config, not the effective
merged catalogue. use --config .lsops.json when editing local entries.

## live hooks

install an output-mode hook against the base catalogue as usual:

```sh
lsops hook install claude --mode output
```

each render selects the project using the hook process's current working directory
and rereads its file. an already installed hook needs no rewrite after project
registration or catalogue edits. OpenCode's generated plugin passes the session
directory as the subprocess cwd; command hooks use the working directory supplied
by their harness. if a harness launches from elsewhere, that other directory is
used, not a guessed active editor folder. this does not claim live authenticated
end-to-end verification for every harness.

instructions-mode hooks still ask the agent to invoke lsops; selection then uses
that invocation's cwd. selection is event-driven, not background polling.

## turn it off

```sh
lsops project remove /absolute/path/to/project
```

removal only unregisters the directory. it leaves its file, global tools and hook
settings untouched. keep the base config outside repositories you do not trust;
only its registered roots can select overlays. opting in trusts future catalogue
edits within that project. guidance cannot grant an agent tool permissions.
older lsops binaries reject the new projects field: remove project registrations
with the updated version before downgrading.
