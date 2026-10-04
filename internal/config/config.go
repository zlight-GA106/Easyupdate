package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Listen    string `yaml:"listen"`
		PublicURL string `yaml:"public_url"`
	} `yaml:"server"`
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	Storage struct {
		Path        string `yaml:"path"`
		MaxUploadMB int64  `yaml:"max_upload_mb"`
	} `yaml:"storage"`
	Admin struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"admin"`
	GitHub struct {
		Token string `yaml:"token"`
	} `yaml:"github"`
}

func Load(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read config: %w; copy config.example.yaml to config.yaml", err)
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err = d.Decode(&c); err != nil {
		return c, fmt.Errorf("parse config: %w", err)
	}
	c.Server.PublicURL = strings.TrimRight(c.Server.PublicURL, "/")
	u, err := url.Parse(c.Server.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return c, fmt.Errorf("server.public_url must be an absolute HTTP(S) origin")
	}
	if _, _, err = net.SplitHostPort(c.Server.Listen); err != nil {
		return c, fmt.Errorf("server.listen: %w", err)
	}
	if c.Database.Path == "" || c.Storage.Path == "" {
		return c, fmt.Errorf("database.path and storage.path are required")
	}
	if c.Admin.Username == "" || len(c.Admin.Username) > 128 || c.Admin.Password == "" || len(c.Admin.Password) > 72 {
		return c, fmt.Errorf("admin username and password are required (password: 1–72 bytes)")
	}
	if c.Storage.MaxUploadMB == 0 {
		c.Storage.MaxUploadMB = 512
	}
	if c.Storage.MaxUploadMB < 1 || c.Storage.MaxUploadMB > 4096 {
		return c, fmt.Errorf("storage.max_upload_mb must be 1–4096")
	}
	return c, nil
}
