package integrate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxManagedFileSize = 8 << 20

func inspectManagedFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlinked agent settings %q; existing file is unchanged", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("agent settings %q are not a regular file; existing file is unchanged", path)
	}
	return info, nil
}

func readManagedFile(path string) ([]byte, error) {
	info, err := inspectManagedFile(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxManagedFileSize {
		return nil, fmt.Errorf("agent settings %q exceed the 8 MiB limit; existing file is unchanged", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open agent settings %q: %w", path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxManagedFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read agent settings %q: %w", path, err)
	}
	if len(data) > maxManagedFileSize {
		return nil, fmt.Errorf("agent settings %q exceed the 8 MiB limit; existing file is unchanged", path)
	}
	return data, nil
}

// replaceManagedFile stages all bytes and a durable backup before replacement.
// An optimistic recheck prevents overwriting a change made since the merge.
// Backup suffixes deliberately do not end in .json, .js, or .ts: agents must
// not discover a backup as another live hook/plugin.
func replaceManagedFile(path string, original, updated []byte, existed, remove bool) (string, error) {
	if !remove && len(updated) > maxManagedFileSize {
		return "", errors.New("updated agent settings would exceed the 8 MiB limit")
	}
	if err := unchangedFile(path, original, existed); err != nil {
		return "", err
	}
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create integration directory %q: %w", dir, err)
	}
	mode := os.FileMode(0600)
	if existed {
		info, err := inspectManagedFile(path)
		if err != nil {
			return "", err
		}
		mode = info.Mode().Perm()
	}
	var staged string
	if !remove {
		var err error
		staged, err = stageFile(dir, ".lsops-integration-*", updated, mode)
		if err != nil {
			return "", err
		}
		defer os.Remove(staged)
	}
	backup := ""
	if existed {
		var err error
		backup, err = stageFile(dir, base+".lsops-backup-*", original, 0600)
		if err != nil {
			return "", fmt.Errorf("back up %q before changing it: %w; existing file is unchanged", path, err)
		}
	}
	if err := unchangedFile(path, original, existed); err != nil {
		if backup != "" {
			_ = os.Remove(backup)
		}
		return "", err
	}
	var err error
	if remove {
		err = os.Remove(path)
	} else {
		err = os.Rename(staged, path)
	}
	if err != nil {
		if backup != "" {
			_ = os.Remove(backup)
		}
		return "", fmt.Errorf("replace agent settings %q: %w; existing file is unchanged", path, err)
	}
	return backup, nil
}

func unchangedFile(path string, original []byte, existed bool) error {
	current, err := readManagedFile(path)
	if !existed && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recheck agent settings %q: %w; existing file is unchanged", path, err)
	}
	if !existed || !bytes.Equal(current, original) {
		return fmt.Errorf("agent settings %q changed during installation; retry after reviewing the change; existing file is unchanged", path)
	}
	return nil
}

func stageFile(dir, pattern string, data []byte, mode os.FileMode) (path string, err error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		_ = file.Close()
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if err = file.Chmod(mode); err != nil {
		return path, err
	}
	if _, err = file.Write(data); err != nil {
		return path, err
	}
	if err = file.Sync(); err != nil {
		return path, err
	}
	if err = file.Close(); err != nil {
		return path, err
	}
	return path, nil
}
