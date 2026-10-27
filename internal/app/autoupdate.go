package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"time"

	"github.com/Microck/lsops/internal/config"
	"github.com/Microck/lsops/internal/manage"
)

func approvalsPath(path string) string { return path + ".updates.json" }

func loadApprovals(path string) (config.Config, error) {
	path = approvalsPath(path)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return config.Config{}, errors.New("update approvals must be a regular file, not a symlink")
	}
	return config.Load(path)
}

// automaticPlans excludes deleted, hidden or changed records. Editing or
// importing a link never silently broadens a prior approval.
func automaticPlans(base, approved config.Config) ([]installPlan, []string, error) {
	var names []string
	for n := range approved.Tools {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []installPlan
	var skipped []string
	for _, n := range names {
		t, ok := base.Tools[n]
		a := approved.Tools[n]
		if !ok || t.Hide || t.Description == "" || t.InstallURL == "" || t.InstallURL != a.InstallURL {
			skipped = append(skipped, n)
			continue
		}
		p, err := plans(base, []string{n}, false, true)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, p...)
	}
	return out, skipped, nil
}

func autoUpdateCommand(args []string, opts options, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return fail(stderr, "usage: lsops auto-update enable|disable|status|run (use --help after the action)", 2)
	}
	action := args[0]
	fs := flag.NewFlagSet("auto-update "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var yes, all, once bool
	interval := 24 * time.Hour
	switch action {
	case "enable":
		fs.BoolVar(&yes, "yes", false, "approve these exact package links for unattended execution")
		fs.BoolVar(&all, "all", false, "approve all base tools with install links")
	case "disable":
		fs.BoolVar(&all, "all", false, "remove all automatic-update approvals")
	case "status":
	case "run":
		fs.BoolVar(&once, "once", false, "run one pass, for an external scheduler")
		fs.DurationVar(&interval, "interval", 24*time.Hour, "time between completed passes, minimum 1h")
	default:
		return fail(stderr, "unknown auto-update action", 2)
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if (action == "run" || action == "status") && fs.NArg() != 0 {
		return fail(stderr, "unexpected arguments", 2)
	}
	if action == "run" {
		if interval < time.Hour {
			return fail(stderr, "auto-update interval must be at least 1h", 2)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		err := autoLoop(ctx, opts.configPath, interval, once, manage.Execute, stdout, stderr)
		if err != nil && !errors.Is(err, context.Canceled) {
			return fail(stderr, err.Error(), 1)
		}
		return 0
	}
	if action == "enable" {
		cfg, err := config.Load(opts.configPath)
		if err != nil {
			return fail(stderr, err.Error(), 1)
		}
		plan, err := plans(cfg, fs.Args(), all, true)
		if err != nil {
			return fail(stderr, err.Error(), 2)
		}
		printPlans(stdout, plan)
		if !yes {
			fmt.Fprintln(stdout, "Preview only. --yes approves future package install/build scripts and potentially breaking latest versions for these exact links. It does not start a worker or install a service.")
			return 0
		}
		err = withUpdateLock(opts.configPath, func() error {
			approved, err := loadApprovals(opts.configPath)
			if err != nil {
				return err
			}
			for _, p := range plan {
				approved.Tools[p.Name] = config.Tool{Description: cfg.Tools[p.Name].Description, InstallURL: p.Link}
			}
			return config.Save(approvalsPath(opts.configPath), approved)
		})
		if err != nil {
			return fail(stderr, err.Error(), 1)
		}
		fmt.Fprintln(stdout, "Approved. Run lsops auto-update run for periodic updates, or schedule auto-update run --once. No service was installed.")
		return 0
	}
	if action == "disable" {
		if all && fs.NArg() > 0 || !all && fs.NArg() == 0 {
			return fail(stderr, "choose names or --all", 2)
		}
		err := withUpdateLock(opts.configPath, func() error {
			approved, err := loadApprovals(opts.configPath)
			if err != nil {
				return err
			}
			if all {
				approved.Tools = map[string]config.Tool{}
			} else {
				for _, n := range fs.Args() {
					delete(approved.Tools, n)
				}
			}
			return config.Save(approvalsPath(opts.configPath), approved)
		})
		if err != nil {
			return fail(stderr, err.Error(), 1)
		}
		fmt.Fprintln(stdout, "Disabled. An already-running package operation may finish; future passes will skip disabled tools.")
		return 0
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	approved, err := loadApprovals(opts.configPath)
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	plan, skipped, err := automaticPlans(cfg, approved)
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	printPlans(stdout, plan)
	for _, n := range skipped {
		fmt.Fprintf(stdout, "%s: suspended (removed, hidden or install link changed); explicitly enable again after review\n", displayName(n))
	}
	fmt.Fprintf(stdout, "%d eligible, %d suspended. Approvals do not indicate that a worker/service is running.\n", len(plan), len(skipped))
	return 0
}

func autoPass(ctx context.Context, path string, run manage.Runner, stdout, stderr io.Writer) (bool, error) {
	active := false
	err := withUpdateLock(path, func() error {
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		approved, err := loadApprovals(path)
		if err != nil {
			return err
		}
		plan, skipped, err := automaticPlans(cfg, approved)
		if err != nil {
			return err
		}
		for _, n := range skipped {
			fmt.Fprintf(stderr, "%s: update suspended; install link no longer matches approval\n", displayName(n))
		}
		active = len(plan) > 0
		return executePlans(ctx, plan, run, stdout, stderr)
	})
	return active, err
}

func autoLoop(ctx context.Context, path string, interval time.Duration, once bool, run manage.Runner, stdout, stderr io.Writer) error {
	for {
		active, err := autoPass(ctx, path, run, stdout, stderr)
		if once {
			return err
		}
		if err != nil {
			fmt.Fprintf(stderr, "Update pass failed: %s\n", cleanText(err.Error(), 1000))
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !active && err == nil {
			fmt.Fprintln(stdout, "No eligible auto-update approvals; stopping.")
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
