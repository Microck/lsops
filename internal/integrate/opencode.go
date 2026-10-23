package integrate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

const openCodeMarker = "// Managed by lsops --managed-lsops-hook"

// openCodePath returns an automatically discovered local plugin path. Options
// are normalized by the integration entry points; this helper never reads env.
func openCodePath(opts Options) (string, error) {
	var base string
	switch opts.Scope {
	case "user":
		base = opts.OpenCodeHome
		if base == "" {
			if !filepath.IsAbs(opts.Home) {
				return "", fmt.Errorf("OpenCode user home must be an absolute path")
			}
			base = joinPath(opts.Home, ".config", "opencode")
		}
	case "project":
		if !filepath.IsAbs(opts.ProjectDir) {
			return "", fmt.Errorf("OpenCode project directory must be an absolute path")
		}
		base = joinPath(opts.ProjectDir, ".opencode")
	default:
		return "", fmt.Errorf("unknown OpenCode integration scope %q", opts.Scope)
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("OpenCode configuration directory must be an absolute path")
	}
	return joinPath(base, "plugins", "lsops.js"), nil
}

func openCodeContent(opts Options) ([]byte, error) {
	for _, path := range []struct{ name, value string }{
		{"executable", opts.Executable},
		{"configuration", opts.ConfigPath},
	} {
		if err := safePath(path.value, "OpenCode "+path.name); err != nil {
			return nil, err
		}
		if !filepath.IsAbs(path.value) && !(opts.GOOS == "windows" && windowsAbsolute(path.value)) {
			return nil, fmt.Errorf("OpenCode %s must be an absolute path", path.name)
		}
	}
	if opts.Mode != "instructions" && opts.Mode != "output" {
		return nil, fmt.Errorf("unknown OpenCode integration mode %q", opts.Mode)
	}
	if opts.Limit < 0 {
		return nil, fmt.Errorf("OpenCode integration limit must not be negative")
	}
	executable, err := json.Marshal(opts.Executable)
	if err != nil {
		return nil, err
	}
	args, err := json.Marshal(hookArgs("opencode", "context", opts))
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(openCodeTemplate, openCodeMarker, executable, args)), nil
}

// OpenCode's Plugin.define is an identity helper. A plain JavaScript object
// avoids a runtime package dependency while exposing both documented APIs:
// V1 >= 1.18.29 calls server(), and V2 calls setup(). Each model request gets
// fresh context; no conversation transcript is changed by these transforms.
const openCodeTemplate = `%s
// OpenCode 1.18.29+ and V2. Reinstall with lsops to change these settings.
import { execFile } from "node:child_process";
import { stat } from "node:fs/promises";
import { isAbsolute } from "node:path";

const executable = %s;
const args = %s;

function currentDirectory() {
  try {
    return process.cwd();
  } catch {
    return undefined;
  }
}

async function sessionDirectory(lookup) {
  if (!lookup) return currentDirectory();
  let timer;
  try {
    return await Promise.race([
      (async () => {
        const response = await lookup();
        const session = response?.data ?? response;
        const directory = session?.location?.directory ?? session?.directory;
        if (typeof directory !== "string" || !isAbsolute(directory) || directory.includes("\0")) {
          return currentDirectory();
        }
        const info = await stat(directory);
        return info.isDirectory() ? directory : currentDirectory();
      })(),
      new Promise((resolve) => {
        timer = setTimeout(() => resolve(currentDirectory()), 1000);
      }),
    ]);
  } catch {
    return currentDirectory();
  } finally {
    clearTimeout(timer);
  }
}

function renderContext(cwd) {
  return new Promise((resolve) => {
    execFile(executable, args, {
      cwd,
      encoding: "utf8",
      timeout: 10000,
      maxBuffer: 256 * 1024,
      windowsHide: true,
      shell: false,
    }, (error, stdout) => {
      if (error) {
        // Never copy child stderr or command arguments into model context.
        console.warn("[lsops] Could not refresh CLI context. Run lsops to check the configuration.");
        resolve("");
        return;
      }
      resolve(typeof stdout === "string" ? stdout.trim() : "");
    });
  });
}

export default {
  id: "lsops",

  async setup(ctx) {
    const registration = await ctx.session.hook("context", async (event) => {
      const lookup = event.sessionID && typeof ctx.session.get === "function"
        ? () => ctx.session.get({ sessionID: event.sessionID })
        : undefined;
      const text = await renderContext(await sessionDirectory(lookup));
      if (text) event.system.push({ type: "text", text });
    });
    return () => registration.dispose();
  },

  async server(ctx) {
    return {
      "experimental.chat.system.transform": async (input, output) => {
        const lookup = input.sessionID && typeof ctx.client?.session?.get === "function"
          ? () => ctx.client.session.get({ path: { id: input.sessionID } })
          : undefined;
        const text = await renderContext(await sessionDirectory(lookup));
        if (text) output.system.push(text);
      },
    };
  },
};
`
