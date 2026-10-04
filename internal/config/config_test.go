package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationValidation(t *testing.T) {
	original, err := os.ReadFile("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		data  string
		valid bool
	}{
		{"example", string(original), true},
		{"missing origin", strings.Replace(string(original), "http://127.0.0.1:8080", "invalid", 1), false},
		{"empty password", strings.Replace(string(original), `password: "admin"`, `password: ""`, 1), false},
		{"unknown key", string(original) + "unknown: true\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			os.WriteFile(path, []byte(tc.data), 0600)
			_, err := Load(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t, err=%v", tc.valid, err)
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil || !strings.Contains(err.Error(), "config.example.yaml") {
		t.Fatal("missing configuration guidance")
	}
}
