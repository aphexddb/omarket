package client

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const sellerTokenFileMode = 0o600

func sellerTokenPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "seller_token"), nil
}

// SaveSellerToken writes the seller token to ConfigDir()/seller_token
// (0600), creating the config directory if needed.
func SaveSellerToken(token string) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, err := sellerTokenPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), sellerTokenFileMode)
}

// LoadSellerToken reads the stored seller token. A missing file is not an
// error; it yields an empty string.
func LoadSellerToken() (string, error) {
	path, err := sellerTokenPath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// HasSellerToken reports whether a seller token is stored.
func HasSellerToken() bool {
	tok, err := LoadSellerToken()
	return err == nil && tok != ""
}
