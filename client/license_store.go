package client

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const licenseFileMode = 0o600

// LicensesDir returns ConfigDir()/licenses.
func LicensesDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "licenses"), nil
}

func licensePath(app string) (string, error) {
	dir, err := LicensesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, app+".key"), nil
}

// SaveLicense writes a trimmed license key to licenses/<app>.key (0600),
// creating the licenses directory if needed.
func SaveLicense(app, key string) error {
	dir, err := LicensesDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, err := licensePath(app)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(key)+"\n"), licenseFileMode)
}

// LoadLicense reads and trims the stored key for app.
func LoadLicense(app string) (string, error) {
	path, err := licensePath(app)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// HasLicense reports whether a license key is stored for app.
func HasLicense(app string) bool {
	path, err := licensePath(app)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// LicenseEntry describes one stored license file.
type LicenseEntry struct {
	App  string
	Path string
	Key  string
}

// ListLicenses returns all stored licenses, sorted by app id. A missing
// licenses directory yields an empty (nil) slice, not an error.
func ListLicenses() ([]LicenseEntry, error) {
	dir, err := LicensesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []LicenseEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".key") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		out = append(out, LicenseEntry{
			App:  strings.TrimSuffix(e.Name(), ".key"),
			Path: path,
			Key:  strings.TrimSpace(string(b)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
	return out, nil
}
