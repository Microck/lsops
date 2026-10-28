package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	bridge "github.com/Microck/lsops/internal/canonfig"
)

func canonfigCommand(args []string, opts options, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stdout, "usage: lsops [--config FILE] [--json] canonfig capture|sync --bundle FILE [--apply] [--watch] [--interval 30s]\nPreview is default. Capture exports portable metadata; sync preserves follower-only tools and stops on conflicts. No install, uninstall, publication, credentials or update approvals. Watch is an opt-in foreground worker. Stop other catalogue writers first.")
		return 0
	}
	command := args[0]
	if command != "capture" && command != "sync" {
		return fail(stderr, "expected canonfig capture or sync", 2)
	}
	fs := flag.NewFlagSet("canonfig "+command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	bundle := fs.String("bundle", "", "shared staging catalogue (not the live config)")
	apply := fs.Bool("apply", false, "apply the reviewed changes")
	watch := fs.Bool("watch", false, "repeat in foreground, stopping on errors or conflicts")
	interval := fs.Duration("interval", 30*time.Second, "poll interval, 1s..24h")
	if e := fs.Parse(args[1:]); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || *bundle == "" {
		return fail(stderr, "--bundle FILE is required; no positional arguments", 2)
	}
	explicitInterval := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "interval" {
			explicitInterval = true
		}
	})
	if *watch && !*apply {
		return fail(stderr, "--watch requires --apply", 2)
	}
	if *interval < time.Second || *interval > 24*time.Hour || explicitInterval && !*watch {
		return fail(stderr, "--interval requires --watch and must be 1s..24h", 2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runCanonfig(ctx, command, bridge.Options{Config: opts.configPath, Bundle: *bundle, Apply: *apply}, *watch, *interval, opts.json, stdout, stderr)
}
func runCanonfig(ctx context.Context, command string, o bridge.Options, watch bool, interval time.Duration, jsonOutput bool, stdout, stderr io.Writer) int {
	for {
		if ctx.Err() != nil {
			return 0
		}
		var r bridge.Result
		var e error
		if command == "capture" {
			r, e = bridge.Capture(o)
		} else {
			r, e = bridge.Sync(o)
		}
		if e != nil {
			return fail(stderr, e.Error(), 1)
		}
		if jsonOutput {
			if e = json.NewEncoder(stdout).Encode(r); e != nil {
				return fail(stderr, e.Error(), 1)
			}
		} else {
			if len(r.Conflicts) > 0 {
				fmt.Fprintln(stdout, "Nothing applied. Conflicts:")
				for _, c := range r.Conflicts {
					fmt.Fprintln(stdout, c)
				}
			} else {
				fmt.Fprintf(stdout, "%s: changed=%t applied=%t; added=%d updated=%d removed=%d. No CLI installed or uninstalled.\n", command, r.Changed, r.Applied, len(r.Added), len(r.Updated), len(r.Removed))
				if !o.Apply {
					fmt.Fprintln(stdout, "Preview only; use --apply to write after review.")
				}
			}
		}
		if len(r.Conflicts) > 0 {
			return 1
		}
		if !watch {
			return 0
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
	}
}
