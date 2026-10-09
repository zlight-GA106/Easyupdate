package storage

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func zipFixture(t *testing.T, names, contents []string) []byte {
	t.Helper()
	var output bytes.Buffer
	z := zip.NewWriter(&output)
	for i, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(contents[i])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestZIPMetadataStorageAndDeletion(t *testing.T) {
	local, _ := New(t.TempDir(), 1<<20)
	data := zipFixture(t, []string{"easyupdate.json", "Demo.exe"}, []string{`{"package_name":"com.example.demo","version_name":"1.7.0","version_code":10700}`, "binary"})
	file, err := local.StageArtifact(bytes.NewReader(data), "zip")
	if err != nil {
		t.Fatal(err)
	}
	if file.Metadata.Source != "easyupdate.json" || file.Metadata.VersionCode != 10700 || file.ArtifactType != "zip" {
		t.Fatalf("metadata: %+v", file)
	}
	stored, err := local.Commit(file, 1, 10700)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(stored) != "app.zip" {
		t.Fatal(stored)
	}
	if _, err := local.Commit(file, 1, 10700); err == nil {
		t.Fatal("overwrote version")
	}
	if err := local.Delete(1, 10700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stored); !os.IsNotExist(err) {
		t.Fatal("ZIP was not deleted")
	}
}

func TestZIPValidationAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name            string
		names, contents []string
		valid           bool
	}{
		{"manual metadata", []string{"Demo.exe"}, []string{"binary"}, true},
		{"traversal", []string{"../bad.exe"}, []string{"binary"}, false},
		{"absolute", []string{"/bad.exe"}, []string{"binary"}, false},
		{"backslash", []string{`dir\bad.exe`}, []string{"binary"}, false},
		{"duplicates", []string{"Demo.exe", "demo.exe"}, []string{"a", "b"}, false},
		{"bad version", []string{"easyupdate.json"}, []string{`{"package_name":"com.example.demo","version_name":"1","version_code":0}`}, false},
		{"invalid JSON", []string{"easyupdate.json"}, []string{`broken`}, false},
		{"trailing JSON", []string{"easyupdate.json"}, []string{`{"package_name":"com.example.demo","version_name":"1","version_code":1} {}`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local, _ := New(t.TempDir(), 1<<20)
			file, err := local.StageArtifact(bytes.NewReader(zipFixture(t, tc.names, tc.contents)), "zip")
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if err == nil {
				local.Discard(file)
			}
			entries, _ := os.ReadDir(filepath.Join(local.Root, ".staging"))
			if len(entries) != 0 {
				t.Fatal("temporary file leaked")
			}
		})
	}
}
