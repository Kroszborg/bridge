package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// fileConfig is what `bridgectl login` saves.
type fileConfig struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
}

// resolved is the configuration in effect: flags, then environment, then file.
type resolved struct {
	URL    string
	APIKey string
	Source string // where the key came from, for `whoami`
}

func configPath() (string, error) {
	if p := os.Getenv("BRIDGE_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bridge", "config.json"), nil
}

func loadFile() (fileConfig, error) {
	var cfg fileConfig
	p, err := configPath()
	if err != nil {
		return cfg, err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("%s is not valid JSON: %w", p, err)
	}
	return cfg, nil
}

// saveFile writes the config readable only by the current user.
func saveFile(cfg fileConfig) (string, error) {
	p, err := configPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}

func removeFile() (string, error) {
	p, err := configPath()
	if err != nil {
		return "", err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return p, nil
}

func resolve(flagURL, flagKey string) (resolved, error) {
	file, err := loadFile()
	if err != nil {
		return resolved{}, err
	}
	r := resolved{URL: file.URL, APIKey: file.APIKey, Source: "config file"}
	if v := os.Getenv("BRIDGE_URL"); v != "" {
		r.URL = v
	}
	if v := os.Getenv("BRIDGE_API_KEY"); v != "" {
		r.APIKey, r.Source = v, "BRIDGE_API_KEY"
	}
	if flagURL != "" {
		r.URL = flagURL
	}
	if flagKey != "" {
		r.APIKey, r.Source = flagKey, "--api-key"
	}
	if r.URL == "" {
		r.URL = "http://localhost:8080"
	}
	r.URL = strings.TrimRight(r.URL, "/")
	return r, nil
}

func (r resolved) requireKey() error {
	if r.APIKey == "" {
		return errors.New("no API key. Run `bridgectl login`, set BRIDGE_API_KEY, or pass --api-key")
	}
	return nil
}
