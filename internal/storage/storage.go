package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var ErrTooLarge = errors.New("file too large")

type Local struct {
	Root     string
	MaxBytes int64
}
type Staged struct {
	Path, SHA256 string
	ArtifactType string
	Size         int64
	Metadata     Metadata
}

func New(root string, max int64) (*Local, error) {
	path, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(path, ".staging"), 0700); err != nil {
		return nil, err
	}
	s := &Local{Root: path, MaxBytes: max}
	s.Cleanup()
	return s, nil
}
func (s *Local) Cleanup() {
	entries, _ := os.ReadDir(filepath.Join(s.Root, ".staging"))
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && !e.IsDir() && time.Since(info.ModTime()) > time.Hour {
			os.Remove(filepath.Join(s.Root, ".staging", e.Name()))
		}
	}
}
func (s *Local) Stage(r io.Reader) (Staged, error) {
	return s.StageArtifact(r, "apk")
}
func (s *Local) StageArtifact(r io.Reader, kind string) (Staged, error) {
	var result Staged
	if kind != "apk" && kind != "zip" {
		return result, errors.New("unsupported artifact type")
	}
	result.ArtifactType = kind
	f, err := os.CreateTemp(filepath.Join(s.Root, ".staging"), "*."+kind)
	if err != nil {
		return result, fmt.Errorf("temporary APK: %w", err)
	}
	result.Path = f.Name()
	success := false
	defer func() {
		f.Close()
		if !success {
			os.Remove(result.Path)
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(r, s.MaxBytes+1))
	if err != nil {
		return result, fmt.Errorf("save APK: %w", err)
	}
	if n > s.MaxBytes {
		return result, ErrTooLarge
	}
	if n == 0 {
		return result, errors.New("empty file")
	}
	if err = f.Sync(); err != nil {
		return result, err
	}
	if err = f.Close(); err != nil {
		return result, err
	}
	if kind == "zip" {
		result.Metadata, err = ReadZIPMetadata(result.Path)
		if err != nil {
			return result, err
		}
	} else {
		if err = ValidateAPK(result.Path); err != nil {
			return result, err
		}
		result.Metadata, _ = ReadMetadata(result.Path)
	}
	result.Size = n
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	success = true
	return result, nil
}
func (s *Local) Path(appID, code int64) string {
	return s.ArtifactPath(appID, code, "apk")
}
func (s *Local) ArtifactPath(appID, code int64, kind string) string {
	if kind != "zip" {
		kind = "apk"
	}
	return filepath.Join(s.Root, strconv.FormatInt(appID, 10), strconv.FormatInt(code, 10), "app."+kind)
}
func (s *Local) Commit(staged Staged, appID, code int64) (string, error) {
	if appID <= 0 || code <= 0 {
		return "", errors.New("invalid storage identity")
	}
	destination := s.ArtifactPath(appID, code, staged.ArtifactType)
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(filepath.Dir(parent), 0700); err != nil {
		return "", err
	}
	// Mkdir, rather than MkdirAll, prevents replacing an existing version.
	if err := os.Mkdir(parent, 0700); err != nil {
		return "", fmt.Errorf("version directory: %w", err)
	}
	if err := os.Rename(staged.Path, destination); err != nil {
		os.Remove(parent)
		return "", fmt.Errorf("move APK: %w", err)
	}
	return destination, nil
}
func (s *Local) Discard(staged Staged) {
	if filepath.Dir(staged.Path) == filepath.Join(s.Root, ".staging") {
		os.Remove(staged.Path)
	}
}
func (s *Local) Delete(appID, code int64) error {
	p := s.Path(appID, code)
	for _, kind := range []string{"apk", "zip"} {
		if err := os.Remove(s.ArtifactPath(appID, code, kind)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return removeEmpty(filepath.Dir(p))
}
func removeEmpty(p string) error {
	err := os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
