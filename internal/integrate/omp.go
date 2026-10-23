package integrate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

const ompMarker = "// Managed by lsops --managed-lsops-hook omp"

func ompPath(opts Options) (string, error) {
	var base string
	if opts.Scope == "project" {
		base = joinPath(opts.ProjectDir, ".omp")
	} else {
		base = opts.OMPHome
		if base == "" {
			base = joinPath(opts.Home, ".omp", "agent")
		}
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("OMP agent directory must be an absolute path")
	}
	return joinPath(base, "extensions", "lsops.js"), nil
}

func ompContent(opts Options) ([]byte, error) {
	// Reuse option validation without inheriting OpenCode's API or marker.
	if _, err := openCodeContent(opts); err != nil {
		return nil, err
	}
	executable, err := json.Marshal(opts.Executable)
	if err != nil {
		return nil, err
	}
	args, err := json.Marshal(hookArgs("omp", "context", opts))
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(ompTemplate, ompMarker, executable, args)), nil
}

const ompTemplate = `%s
// OMP ExtensionAPI session lifecycle hook. Reinstall with lsops to change settings.
import { execFile } from "node:child_process";
import { isAbsolute } from "node:path";

const executable = %s;
const args = %s;
const customType = "lsops.catalogue";

function renderContext(cwd) {
  return new Promise((resolve) => {
    execFile(executable, args, {
      cwd, encoding: "utf8", timeout: 10000, maxBuffer: 256 * 1024,
      windowsHide: true, shell: false,
    }, (error, stdout) => {
      if (error) {
        console.warn("[lsops] Could not refresh CLI context. Run lsops to check the configuration.");
        resolve("");
        return;
      }
      resolve(typeof stdout === "string" ? stdout.trim() : "");
    });
  });
}

export default function lsops(pi) {
  let generation = 0;
  async function refresh(_event, ctx) {
    const current = ++generation;
    const cwd = ctx?.cwd;
    if (typeof cwd !== "string" || !isAbsolute(cwd) || cwd.includes("\0")) return;
    const text = await renderContext(cwd);
    // Never deliver an old project's result after a session switch or shutdown.
    if (current !== generation || !text) return;
    pi.sendMessage({ customType, content: text, display: false },
      { triggerTurn: false });
  }
  // Persist one message at a lifecycle boundary. Ordinary model/tool turns do
  // not render, append, replace, or relocate the catalogue.
  for (const event of ["session_start", "session_switch", "session_branch",
    "session_tree", "session_compact"]) pi.on(event, refresh);
  pi.on("session_shutdown", () => { generation++; });
}
`
