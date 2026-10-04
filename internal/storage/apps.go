package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// DeleteApp removes only this application's directory and the staged files
// supplied by the server after invalidating their pending upload tokens.
func (s *Local) DeleteApp(appID int64, pending ...Staged) error {
	if appID <= 0 {
		return errors.New("invalid storage identity")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return err
	}
	name := strconv.FormatInt(appID, 10)
	directory := filepath.Join(root, name)
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative != name {
		return errors.New("application directory is outside storage")
	}
	var failures []error
	// RemoveAll does not traverse symlinks, including links inside this directory.
	if err = os.RemoveAll(directory); err != nil {
		failures = append(failures, fmt.Errorf("application APK directory: %w", err))
	}
	staging := filepath.Join(root, ".staging")
	for _, file := range pending {
		path, err := filepath.Abs(file.Path)
		if err != nil || filepath.Dir(path) != staging {
			failures = append(failures, errors.New("pending APK is outside staging"))
			continue
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("pending APK: %w", err))
		}
	}
	return errors.Join(failures...)
}
