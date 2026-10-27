package app

import (
	"context"
	"fmt"
	"github.com/Microck/lsops/internal/manage"
	"io"
	"os"
)

// script fetch never runs the downloaded contents, writes only a fresh private
// file, and prints its digest for review before the separate registration step.
func scriptCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 3 || args[0] != "fetch" {
		return fail(stderr, "usage: lsops script fetch HTTPS_URL LOCAL_FILE (download for review, never execute)", 2)
	}
	if _, err := os.Lstat(args[2]); err == nil || !os.IsNotExist(err) {
		return fail(stderr, "output must be a new file, not an existing file or symlink", 1)
	}
	data, err := manage.DownloadScript(context.Background(), args[1], manage.ScriptClient())
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	f, err := os.OpenFile(args[2], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(args[2])
		return fail(stderr, "could not save complete installer", 1)
	}
	fmt.Fprintf(stdout, "Downloaded only; not executed. Review %q and its publisher before trusting it.\nSHA256: %s\n", args[2], manage.ScriptDigest(data))
	return 0
}
