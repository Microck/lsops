package app

import (
	"fmt"
	"github.com/Microck/lsops/internal/config"
	"io"
)

func projectCommand(args []string, opts options, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "list" {
		cfg, err := config.Load(opts.configPath)
		if err != nil {
			return fail(stderr, err.Error(), 1)
		}
		for _, root := range cfg.Projects {
			if _, err := fmt.Fprintln(stdout, root); err != nil {
				return fail(stderr, err.Error(), 1)
			}
		}
		return 0
	}
	if len(args) != 2 || (args[0] != "add" && args[0] != "remove") {
		return fail(stderr, "usage: lsops project add|remove DIRECTORY; lsops project list", 2)
	}
	if err := config.SetProject(opts.configPath, args[1], args[0] == "add"); err != nil {
		return fail(stderr, err.Error(), 1)
	}
	fmt.Fprintln(stdout, "Project selection updated. Live hooks use their working directory on the next invocation.")
	return 0
}
