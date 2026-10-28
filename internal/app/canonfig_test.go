package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	bridge "github.com/Microck/lsops/internal/canonfig"
	"github.com/Microck/lsops/internal/config"
)

func TestCanonfigCLIRoundTrip(t *testing.T) {
	opts, _ := managedFixture(t)
	bundle := filepath.Join(t.TempDir(), "bundle.json")
	var out, errout bytes.Buffer
	args := []string{"--config", opts.configPath, "--json", "canonfig", "capture", "--bundle", bundle}
	if code := Run(args, &out, &errout, "test"); code != 0 {
		t.Fatal(code, errout.String())
	}
	if _, e := os.Stat(bundle); !os.IsNotExist(e) {
		t.Fatal("preview wrote bundle", e)
	}
	out.Reset()
	if code := Run(append(args, "--apply"), &out, &errout, "test"); code != 0 {
		t.Fatal(code, errout.String())
	}
	var result bridge.Result
	if e := json.Unmarshal(out.Bytes(), &result); e != nil || !result.Applied {
		t.Fatal(e, result)
	}
	follower := filepath.Join(t.TempDir(), "config.json")
	out.Reset()
	if code := Run([]string{"--config", follower, "canonfig", "sync", "--bundle", bundle, "--apply"}, &out, &errout, "test"); code != 0 {
		t.Fatal(code, errout.String())
	}
	c, e := config.Load(follower)
	if e != nil || len(c.Tools) != 3 {
		t.Fatal(c, e)
	}
	if _, e = os.Stat(follower + ".updates.json"); !os.IsNotExist(e) {
		t.Fatal("created approvals", e)
	}
}
func TestCanonfigCLIRejectsInvalidOptions(t *testing.T) {
	opts, _ := managedFixture(t)
	for _, args := range [][]string{{"unknown"}, {"sync"}, {"sync", "--bundle", "x", "--watch"}, {"sync", "--bundle", "x", "--interval", "1s"}, {"sync", "--bundle", "x", "--apply", "--watch", "--interval", "0s"}, {"sync", "--bundle", "x", "--apply", "--watch", "--interval", "25h"}, {"sync", "--bundle", "x", "unexpected"}, {"sync", "--bundle", "x", "--force"}} {
		if code := canonfigCommand(args, opts, io.Discard, io.Discard); code != 2 {
			t.Fatal(args, code)
		}
	}
}

type cancelWriter struct {
	cancel context.CancelFunc
	buf    bytes.Buffer
}

func (w *cancelWriter) Write(b []byte) (int, error) { n, e := w.buf.Write(b); w.cancel(); return n, e }
func TestCanonfigWatchCancellation(t *testing.T) {
	opts, _ := managedFixture(t)
	bundle := filepath.Join(t.TempDir(), "bundle.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &cancelWriter{cancel: cancel}
	if code := runCanonfig(ctx, "capture", bridge.Options{Config: opts.configPath, Bundle: bundle, Apply: true}, true, time.Hour, true, out, io.Discard); code != 0 {
		t.Fatal(code)
	}
	if _, e := os.Stat(bundle + ".capture-lock"); !os.IsNotExist(e) {
		t.Fatal("lock not released", e)
	}
}
