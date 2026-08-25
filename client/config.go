// Package client implements the omarket client: server API access, local
// config, license storage, and package install helpers.
package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// DefaultServer is used when no server is configured by flag, env, or file.
const DefaultServer = "http://localhost:8484"

// omarketServerEnv overrides the configured server; see ResolveServer.
const omarketServerEnv = "OMARKET_SERVER"

// Config is the on-disk shape of config.json.
type Config struct {
	Server string `json:"server"`
}

// ConfigDir returns os.UserConfigDir()/shareware, creating nothing.
func ConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shareware"), nil
}

func configPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LoadConfig reads config.json. A missing file is not an error; it yields a
// zero-value Config.
func LoadConfig() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// SaveConfig writes config.json, creating the shareware directory if needed.
func SaveConfig(c Config) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// ResolveServer applies the server precedence: --server flag value (if
// non-empty) > OMARKET_SERVER env > config file > DefaultServer.
func ResolveServer(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv(omarketServerEnv); v != "" {
		return v
	}
	if cfg, err := LoadConfig(); err == nil && cfg.Server != "" {
		return cfg.Server
	}
	return DefaultServer
}
