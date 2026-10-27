// Package app implements the lsops command-line interface.
package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Microck/lsops/internal/config"
)

type options struct {
	all, json, names, paths, known, context, discover bool
	limit                                             int
	configPath                                        string
}

// Run runs the CLI without exiting the process. It returns a shell exit code.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	var opts options
	var help, showVersion bool
	fs := flag.NewFlagSet("lsops", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&opts.all, "all", false, "include common system commands during discovery")
	fs.BoolVar(&opts.all, "a", false, "include common system commands")
	fs.BoolVar(&opts.json, "json", false, "emit structured JSON")
	fs.BoolVar(&opts.names, "names", false, "print only command names")
	fs.BoolVar(&opts.paths, "paths", false, "show resolved executable paths")
	fs.BoolVar(&opts.known, "known", false, "show only commands with a description")
	fs.BoolVar(&opts.context, "context", false, "add a short agent-facing introduction")
	fs.BoolVar(&opts.discover, "discover", false, "scan PATH instead of showing the curated list")
	fs.IntVar(&opts.limit, "limit", 0, "maximum entries; 0 means no limit")
	fs.StringVar(&opts.configPath, "config", os.Getenv("LSOPS_CONFIG"), "override configuration file")
	fs.BoolVar(&help, "help", false, "show help")
	fs.BoolVar(&help, "h", false, "show help")
	fs.BoolVar(&showVersion, "version", false, "show version")
	fs.Usage = func() { writeUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if help || len(fs.Args()) == 1 && fs.Args()[0] == "help" {
		writeUsage(stdout)
		return 0
	}
	if showVersion {
		fmt.Fprintf(stdout, "lsops %s\n", version)
		return 0
	}
	if opts.limit < 0 {
		return fail(stderr, "--limit must be zero or positive", 2)
	}
	if opts.json && (opts.names || opts.paths || opts.context) {
		return fail(stderr, "--json cannot be combined with --names, --paths, or --context", 2)
	}
	if opts.names && (opts.paths || opts.context) {
		return fail(stderr, "--names cannot be combined with --paths or --context", 2)
	}
	if opts.configPath == "" {
		var err error
		opts.configPath, err = config.DefaultPath()
		if err != nil {
			return fail(stderr, "cannot locate configuration: "+err.Error()+" (use --config)", 1)
		}
	}
	pos := fs.Args()
	if len(pos) > 0 {
		switch pos[0] {
		case "script":
			return scriptCommand(pos[1:], stdout, stderr)
		case "source":
			return sourceCommand(pos[1:], opts, stdout, stderr)
		case "install", "update":
			return installCommand(pos[1:], opts, pos[0] == "update", stdout, stderr)
		case "bundle":
			return bundleCommand(pos[1:], opts, stdout, stderr)
		case "auto-update":
			return autoUpdateCommand(pos[1:], opts, stdout, stderr)
		case "project":
			return projectCommand(pos[1:], opts, stdout, stderr)
		case "add", "describe":
			return addCommand(pos[1:], opts, stdout, stderr)
		case "remove", "forget":
			if len(pos) != 2 {
				return fail(stderr, "usage: lsops [--config FILE] remove NAME", 2)
			}
			removed, err := config.Forget(opts.configPath, pos[1])
			if err != nil {
				return fail(stderr, err.Error(), 1)
			}
			if removed {
				fmt.Fprintf(stdout, "Removed %s from the curated list.\n", displayName(pos[1]))
			} else {
				fmt.Fprintf(stdout, "%s is not configured.\n", displayName(pos[1]))
			}
			return 0
		case "config":
			if len(pos) == 2 && pos[1] == "example" {
				example := config.Config{Version: 1, Tools: map[string]config.Tool{
					"parallel-cli": {Description: "Search the web, extract pages, and run research with Parallel"},
					"gh":           {Description: "Work with GitHub issues and pull requests"},
				}}
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(example); err != nil {
					return fail(stderr, err.Error(), 1)
				}
				return 0
			}
			if len(pos) > 1 {
				return fail(stderr, "usage: lsops config [example]", 2)
			}
			fmt.Fprintln(stdout, opts.configPath)
			return 0
		case "hook":
			return hookCommand(pos[1:], opts, stdout, stderr, version)
		case "discover":
			opts.discover = true
			return listCommand(pos[1:], opts, stdout, stderr, version)
		case "list":
			return listCommand(pos[1:], opts, stdout, stderr, version)
		}
	}
	return list(pos, opts, stdout, stderr, version)
}

func fail(w io.Writer, message string, code int) int {
	fmt.Fprintf(w, "lsops: %s\n", cleanText(message, 1000))
	return code
}

func addCommand(args []string, opts options, stdout, stderr io.Writer) int {
	// Accept --path before or after the positional pair. Never interpret the
	// description as shell code, or execute a registered command.
	var executablePath string
	var why, when, installURL, installManager, installSHA256 *string
	var pos []string
	literal := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !literal && arg == "--" {
			literal = true
			continue
		}
		if !literal && (arg == "--install-manager" || strings.HasPrefix(arg, "--install-manager=") || arg == "--install-sha256" || strings.HasPrefix(arg, "--install-sha256=") || arg == "--install-url" || strings.HasPrefix(arg, "--install-url=") || arg == "--why" || arg == "--when" || strings.HasPrefix(arg, "--why=") || strings.HasPrefix(arg, "--when=")) {
			flag, value, inline := strings.Cut(arg, "=")
			if !inline {
				i++
				if i >= len(args) || strings.HasPrefix(args[i], "--") {
					return fail(stderr, flag+" requires text (use "+flag+"= to clear)", 2)
				}
				value = args[i]
			}
			if flag == "--install-manager" {
				installManager = &value
			} else if flag == "--install-sha256" {
				installSHA256 = &value
			} else if flag == "--install-url" {
				installURL = &value
			} else if flag == "--why" {
				why = &value
			} else {
				when = &value
			}
			continue
		}
		if !literal && (arg == "--path" || strings.HasPrefix(arg, "--path=")) {
			if strings.HasPrefix(arg, "--path=") {
				executablePath = strings.TrimPrefix(arg, "--path=")
			} else {
				i++
				if i >= len(args) {
					return fail(stderr, "--path requires an absolute executable path", 2)
				}
				executablePath = args[i]
			}
			if executablePath == "" {
				return fail(stderr, "--path must not be empty", 2)
			}
			continue
		}
		if !literal && strings.HasPrefix(arg, "--") {
			return fail(stderr, "unknown add option: "+arg, 2)
		}
		pos = append(pos, arg)
	}
	if len(pos) < 2 {
		return fail(stderr, "usage: lsops add NAME \"description\" [--path /absolute/executable] [--why TEXT] [--when TEXT] [--install-url HTTPS_URL]", 2)
	}
	if err := config.SetToolInstallDetails(opts.configPath, pos[0], strings.Join(pos[1:], " "), executablePath, why, when, installURL, installManager, installSHA256); err != nil {
		return fail(stderr, err.Error(), 1)
	}
	fmt.Fprintf(stdout, "Saved %s. Live hooks will use the updated list on their next invocation.\n", displayName(pos[0]))
	return 0
}

func listCommand(args []string, opts options, stdout, stderr io.Writer, version string) int {
	fs := flag.NewFlagSet("lsops list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&opts.all, "all", opts.all, "include common system commands during discovery")
	fs.BoolVar(&opts.all, "a", opts.all, "include common system commands during discovery")
	fs.BoolVar(&opts.json, "json", opts.json, "emit JSON")
	fs.BoolVar(&opts.names, "names", opts.names, "names only")
	fs.BoolVar(&opts.paths, "paths", opts.paths, "show executable paths")
	fs.BoolVar(&opts.known, "known", opts.known, "only described commands")
	fs.BoolVar(&opts.context, "context", opts.context, "add an introduction")
	fs.IntVar(&opts.limit, "limit", opts.limit, "maximum entries; zero is unlimited")
	fs.StringVar(&opts.configPath, "config", opts.configPath, "configuration file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if opts.limit < 0 || opts.json && (opts.names || opts.paths || opts.context) || opts.names && (opts.paths || opts.context) {
		return fail(stderr, "invalid list option combination or negative limit", 2)
	}
	return list(fs.Args(), opts, stdout, stderr, version)
}

func writeUsage(w io.Writer) {
	fmt.Fprint(w, `lsops — your curated CLI catalogue for agents

Usage:
  lsops [options] [filter ...]
  lsops project add|remove DIRECTORY
  lsops project list
  lsops add NAME "description" [--path /absolute/executable] [--why TEXT] [--when TEXT] [--install-url HTTPS_URL]
  lsops source NAME HTTPS_URL [--manager MANAGER] [--sha256 HASH]
  lsops script fetch HTTPS_URL LOCAL_FILE
  lsops install|update [--all] [--yes] [--allow-scripts] [NAME ...]
  lsops bundle export
  lsops bundle import [--yes] FILE_OR_HTTPS_URL
  lsops auto-update enable [--all] [--yes] [--allow-scripts] [NAME ...]
  lsops auto-update disable [--all] [NAME ...]
  lsops auto-update status
  lsops auto-update run [--once] [--interval 24h]
  lsops remove NAME
  lsops discover [options] [filter ...]
  lsops config [example]
  lsops hook install HARNESS [--mode instructions|output] [--scope user|project]
  lsops hook uninstall HARNESS [--scope user|project]
  lsops hook status HARNESS [--scope user|project]
  lsops hook render [--mode instructions|output]
  lsops hook targets

Options:
      --json      Emit the versioned JSON catalogue
      --names     Print only command names
      --paths     Show resolved executable paths
      --context   Add a short introduction for an agent
      --limit N   Show at most N entries (default 0: unlimited)
      --config F  Use configuration F (or set LSOPS_CONFIG)
      --discover  Opt into automatic PATH discovery
  -a, --all       Include common system commands during discovery
      --known     Only described commands during discovery
  -h, --help      Show help
      --version   Print version

The default list contains the CLIs and descriptions you explicitly configure.
Configured tools stay listed if unavailable, with their availability marked.
PATH discovery and automatically inferred descriptions are opt-in via discover.

Examples:
  lsops add parallel-cli "Search the web with Parallel"
  lsops add acme "Query internal analytics" --path /opt/acme/bin/acme
  lsops
  lsops web
  lsops discover --known
  lsops hook install claude --mode output

Global options go before subcommands; list/discover also accept their options
immediately after the subcommand. Filters match names, descriptions, why or when guidance.
Use lsops list FILTER for reserved command names. describe/forget remain aliases
for add/remove. Inventory discovery never executes the registered tools.
`)
}
