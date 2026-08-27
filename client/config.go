// Package client implements the omarket client: server API access, local
// config, license storage, and package install helpers.
package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// DefaultServer is used when no server is configured by flag, env, or file.
const DefaultServer = "https://omarket.dev"

// DefaultPublicKey is the platform's Ed25519 license-signing public key
// (standard base64), also served at https://omarket.dev/api/pubkey. It is
// baked in so a fresh install can verify licenses with zero configuration.
// SHAREWARE_PUBLIC_KEY overrides it for testing or local stacks.
const DefaultPublicKey = "vIODssCr6I8zWUTaK/IhdHzkoK5LTdEzbtIeXRnatko="

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

// PageURL returns the shareable landing page for a listing on server: the
// /a/{id} page the platform serves. Built client-side rather than fetched:
// the path is part of the server's permanent public URL space — it is
// exactly what sellers paste into chats — so it is as stable a contract as
// the API routes themselves.
func PageURL(server, appID string) string {
	return strings.TrimRight(server, "/") + "/a/" + appID
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
