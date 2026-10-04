package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The fixture is the AndroidBinary project's MIT-licensed HelloWorld demo.
func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/helloworld.apk")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestAPKMetadataAndSHA256(t *testing.T) {
	data := fixture(t)
	local, err := New(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := local.Stage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Discard(staged)
	hash := sha256.Sum256(data)
	if staged.SHA256 != hex.EncodeToString(hash[:]) || staged.Size != int64(len(data)) {
		t.Fatal("size or sha256 mismatch")
	}
	if staged.Metadata.PackageName != "com.example.helloworld" || staged.Metadata.VersionCode != 1 {
		t.Fatalf("metadata: %+v", staged.Metadata)
	}
	p, err := local.Commit(staged, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "app.apk" {
		t.Fatal(p)
	}
	if _, err = local.Commit(staged, 1, 1); err == nil {
		t.Fatal("overwrote existing version")
	}
	if err = local.Delete(1, 1); err != nil {
		t.Fatal(err)
	}
}
func TestUploadLimitsAndFormat(t *testing.T) {
	local, _ := New(t.TempDir(), 128)
	if _, err := local.Stage(bytes.NewReader(make([]byte, 129))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit: %v", err)
	}
	if _, err := local.Stage(bytes.NewBufferString("not an apk")); err == nil {
		t.Fatal("accepted invalid apk")
	}
	files, _ := os.ReadDir(filepath.Join(local.Root, ".staging"))
	if len(files) != 0 {
		t.Fatal("temporary file leaked")
	}
}
