// Package config parses the YAML configuration file.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Storage  StorageConfig  `yaml:"storage"`
	TSDB     TSDBConfig     `yaml:"tsdb"`
	AgentHub AgentHubConfig `yaml:"agent_hub"`
}

type ServerConfig struct {
	Listen string `yaml:"listen"`
}

type StorageConfig struct {
	SQLitePath string `yaml:"sqlite_path"`
}

type TSDBConfig struct {
	Mode  string `yaml:"mode"` // local | remote
	Local struct {
		Dir           string `yaml:"dir"`
		RetentionDays int    `yaml:"retention_days"`
	} `yaml:"local"`
	Remote struct {
		WriteURL string `yaml:"write_url"`
		QueryURL string `yaml:"query_url"`
	} `yaml:"remote"`
}

type AgentHubConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Listen    string `yaml:"listen"`
	Transport string `yaml:"transport"` // tcp | quic | udp
}

func defaults() Config {
	var c Config
	c.Server.Listen = ":9800"
	c.Storage.SQLitePath = "data/searchboard.db"
	c.TSDB.Mode = "local"
	c.TSDB.Local.Dir = "data/metrics"
	c.TSDB.Local.RetentionDays = 14
	c.AgentHub.Listen = ":9801"
	c.AgentHub.Transport = "tcp"
	return c
}

// Load reads path (if it exists) over built-in defaults. Missing file is not an error.
func Load(path string) (*Config, error) {
	c := defaults()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &c, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &c, nil
}
