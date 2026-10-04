package storage

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shogo82148/androidbinary"
)

type Metadata struct {
	PackageName, VersionName string
	VersionCode              int64
	Source                   string
}

func ValidateAPK(path string) error {
	z, err := zip.OpenReader(path)
	if err != nil {
		return errors.New("invalid APK ZIP")
	}
	defer z.Close()
	if len(z.File) > 100000 {
		return errors.New("too many APK entries")
	}
	found := false
	var unpacked uint64
	for _, entry := range z.File {
		if entry.UncompressedSize64 > 2<<30 {
			return errors.New("APK entry too large")
		}
		unpacked += entry.UncompressedSize64
		if unpacked > 4<<30 {
			return errors.New("APK expanded size too large")
		}
		if entry.Name == "AndroidManifest.xml" {
			if found {
				return errors.New("duplicate APK manifest")
			}
			found = true
			if entry.UncompressedSize64 > 4<<20 {
				return errors.New("APK manifest too large")
			}
			r, e := entry.Open()
			if e != nil {
				return e
			}
			data, e := io.ReadAll(io.LimitReader(r, (4<<20)+1))
			r.Close()
			if e != nil || len(data) < 8 || len(data) > 4<<20 {
				return errors.New("invalid APK manifest")
			}
		}
	}
	if !found {
		return errors.New("AndroidManifest.xml missing")
	}
	return nil
}

var badging = regexp.MustCompile(`package: name='([^']+)' versionCode='([0-9]+)' versionName='([^']*)'`)

func ReadMetadata(path string) (m Metadata, err error) {
	// External Android SDK tools take priority when installed.
	if tool, e := exec.LookPath("apkanalyzer"); e == nil {
		values := []string{}
		for _, field := range []string{"application-id", "version-name", "version-code"} {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			out, e := exec.CommandContext(ctx, tool, "manifest", field, path).Output()
			cancel()
			if e != nil {
				break
			}
			values = append(values, strings.TrimSpace(string(out)))
		}
		if len(values) == 3 {
			code, e := strconv.ParseInt(values[2], 10, 64)
			if e == nil && code > 0 {
				return Metadata{PackageName: values[0], VersionName: values[1], VersionCode: code, Source: "apkanalyzer"}, nil
			}
		}
	}
	if tool, e := exec.LookPath("aapt2"); e == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, e := exec.CommandContext(ctx, tool, "dump", "badging", path).Output()
		if e == nil {
			matches := badging.FindStringSubmatch(string(out))
			if len(matches) == 4 {
				code, _ := strconv.ParseInt(matches[2], 10, 64)
				if code > 0 {
					return Metadata{PackageName: matches[1], VersionName: matches[3], VersionCode: code, Source: "aapt2"}, nil
				}
			}
		}
	}
	return readBinaryManifest(path)
}
func readBinaryManifest(path string) (m Metadata, err error) {
	// A corrupt binary XML must not bring down the server.
	defer func() {
		if recover() != nil {
			m = Metadata{}
			err = errors.New("unsupported APK manifest")
		}
	}()
	z, err := zip.OpenReader(path)
	if err != nil {
		return m, err
	}
	defer z.Close()
	var data []byte
	for _, f := range z.File {
		if f.Name == "AndroidManifest.xml" {
			r, e := f.Open()
			if e != nil {
				return m, e
			}
			data, e = io.ReadAll(io.LimitReader(r, (4<<20)+1))
			r.Close()
			if e != nil || len(data) > 4<<20 {
				return m, errors.New("invalid APK manifest")
			}
			break
		}
	}
	manifest := struct {
		XMLName          xml.Name `xml:"manifest"`
		Package          string   `xml:"package,attr"`
		VersionName      string   `xml:"http://schemas.android.com/apk/res/android versionName,attr"`
		VersionCode      string   `xml:"http://schemas.android.com/apk/res/android versionCode,attr"`
		VersionCodeMajor string   `xml:"http://schemas.android.com/apk/res/android versionCodeMajor,attr"`
	}{}
	binary, e := androidbinary.NewXMLFile(bytes.NewReader(data))
	if e != nil {
		return m, e
	}
	if e = binary.Decode(&manifest, nil, nil); e != nil {
		return m, e
	}
	code, e := strconv.ParseInt(manifest.VersionCode, 10, 64)
	if e != nil || code <= 0 || manifest.Package == "" {
		return m, fmt.Errorf("unresolved APK version")
	}
	// The MVP uses Android's positive 32-bit versionCode.
	if manifest.VersionCodeMajor != "" && manifest.VersionCodeMajor != "0" {
		return m, errors.New("long version codes are unsupported")
	}
	name := manifest.VersionName
	if strings.HasPrefix(name, "@") || name == "" {
		return Metadata{PackageName: manifest.Package, VersionCode: code, Source: "manifest"}, errors.New("versionName needs manual input")
	}
	return Metadata{PackageName: manifest.Package, VersionName: name, VersionCode: code, Source: "manifest"}, nil
}
