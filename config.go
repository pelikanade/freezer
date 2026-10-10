package main

import (
	"net/url"
	"time"
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
