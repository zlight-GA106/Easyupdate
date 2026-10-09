package storage

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

var zipPackagePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

// ZIP updates remain archives. No uploaded entry is extracted on the server.
// An optional root easyupdate.json supplies immutable version metadata.
func ReadZIPMetadata(filename string) (Metadata, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return Metadata{}, errors.New("invalid update ZIP")
	}
	defer z.Close()
	if len(z.File) == 0 || len(z.File) > 100000 {
		return Metadata{}, errors.New("invalid ZIP entry count")
	}
	var expanded uint64
	var manifest *zip.File
	seen := map[string]bool{}
	for _, entry := range z.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") ||
			path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || !utf8.ValidString(name) || entry.Mode()&os.ModeSymlink != 0 {
			return Metadata{}, errors.New("unsafe ZIP entry name or symbolic link")
		}
		key := strings.ToLower(name)
		if seen[key] {
			return Metadata{}, errors.New("duplicate ZIP entry")
		}
		seen[key] = true
		if entry.UncompressedSize64 > 2<<30 {
			return Metadata{}, errors.New("ZIP entry too large")
		}
		expanded += entry.UncompressedSize64
		if expanded > 4<<30 {
			return Metadata{}, errors.New("ZIP expanded size too large")
		}
		if entry.Name == "easyupdate.json" {
			manifest = entry
		}
	}
	if manifest == nil {
		return Metadata{}, nil
	}
	if manifest.UncompressedSize64 > 4096 {
		return Metadata{}, errors.New("ZIP metadata exceeds 4 KB")
	}
	r, err := manifest.Open()
	if err != nil {
		return Metadata{}, err
	}
	data, err := io.ReadAll(io.LimitReader(r, 4097))
	r.Close()
	if err != nil || len(data) > 4096 {
		return Metadata{}, errors.New("invalid ZIP metadata")
	}
	var value struct {
		PackageName string `json:"package_name"`
		VersionName string `json:"version_name"`
		VersionCode int64  `json:"version_code"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF ||
		!zipPackagePattern.MatchString(value.PackageName) || len(value.PackageName) > 255 ||
		strings.TrimSpace(value.VersionName) == "" || len(value.VersionName) > 128 ||
		value.VersionCode <= 0 || value.VersionCode > 2147483647 {
		return Metadata{}, errors.New("invalid easyupdate.json application/version metadata")
	}
	return Metadata{PackageName: value.PackageName, VersionName: value.VersionName, VersionCode: value.VersionCode, Source: "easyupdate.json"}, nil
}
