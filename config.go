package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Cache      CacheConfig   `yaml:"cache"`
	GlobCap    int           `yaml:"glob_cap"`
	JobTimeout time.Duration `yaml:"job_timeout"`
}

type CacheConfig struct {
	Bucket     string `yaml:"bucket"`
	Endpoint   string `yaml:"endpoint"`
	SigningKey string `yaml:"signing_key"`
}

func (c CacheConfig) StoreURL() string {
	values := url.Values{
		"endpoint": {c.Endpoint}, "region": {"auto"}, "addressing-style": {"path"},
		"compression": {"zstd"}, "secret-key": {c.SigningKey}, "multipart-upload": {"true"},
	}
	return "s3://" + c.Bucket + "?" + values.Encode()
}

func decodeYAML(filename string, value any) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%s: %w", filename, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s: expected one YAML document", filename)
	}
	return nil
}

func LoadConfig(filename string) (Config, error) {
	cfg := Config{GlobCap: 200, JobTimeout: 30 * time.Minute}
	if err := decodeYAML(filename, &cfg); err != nil {
		return cfg, err
	}
	if cfg.GlobCap < 1 || cfg.JobTimeout <= 0 {
		return cfg, fmt.Errorf("glob_cap and job_timeout must be positive")
	}
	if cfg.Cache.Bucket == "" || strings.ContainsAny(cfg.Cache.Bucket, "/?#@:") {
		return cfg, fmt.Errorf("cache.bucket must be one bucket name without a prefix")
	}
	endpoint, err := url.Parse(cfg.Cache.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return cfg, fmt.Errorf("cache.endpoint must be an HTTP(S) endpoint without credentials, query or fragment")
	}
	if cfg.Cache.SigningKey == "" {
		return cfg, fmt.Errorf("cache.signing_key is required")
	}
	if !filepath.IsAbs(cfg.Cache.SigningKey) {
		cfg.Cache.SigningKey, err = filepath.Abs(filepath.Join(filepath.Dir(filename), cfg.Cache.SigningKey))
		if err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func LoadBuildList(directory string) ([]string, error) {
	filename := filepath.Join(directory, ".freezer.yaml")
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf(".freezer.yaml must be a regular file")
	}
	var list struct {
		Entries []string `yaml:"entries"`
	}
	if err := decodeYAML(filename, &list); err != nil {
		return nil, err
	}
	if list.Entries == nil {
		return nil, fmt.Errorf(".freezer.yaml must declare entries")
	}
	for _, entry := range list.Entries {
		if _, _, _, err := parseEntry(entry); err != nil {
			return nil, err
		}
	}
	return list.Entries, nil
}

var attributeName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_'-]*$`)

func parseEntry(entry string) (prefix, pattern string, glob bool, err error) {
	invalid := func() (string, string, bool, error) {
		return "", "", false, fmt.Errorf("invalid entry %q: use an attribute path with an optional final-component glob; output selectors are unsupported", entry)
	}
	var parts []string
	start := 0
	quoted, escaped := false, false
	for i, r := range entry {
		if escaped {
			escaped = false
			continue
		}
		if quoted && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
			continue
		}
		if r == '.' && !quoted {
			parts = append(parts, entry[start:i])
			start = i + 1
		}
	}
	if quoted || escaped {
		return invalid()
	}
	parts = append(parts, entry[start:])
	for i, part := range parts {
		if strings.HasPrefix(part, "\"") {
			name, e := strconv.Unquote(part)
			if e != nil || name == "" || strings.ContainsAny(name, "\n\r") {
				return invalid()
			}
			continue
		}
		if strings.ContainsAny(part, "*?[") {
			if i != len(parts)-1 || i == 0 || strings.ContainsAny(part, "/^#\"\\\n\r") {
				return invalid()
			}
			if _, e := path.Match(part, ""); e != nil {
				return invalid()
			}
			glob = true
		} else if !attributeName.MatchString(part) {
			return invalid()
		}
	}
	if glob {
		return strings.Join(parts[:len(parts)-1], "."), parts[len(parts)-1], true, nil
	}
	return "", "", false, nil
}
