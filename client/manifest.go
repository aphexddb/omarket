package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ManifestFilename is the name of the per-app manifest file that
// `omarket sell claim` generates and `omarket sell push` reads, always in
// the current directory.
const ManifestFilename = "omarket.json"

// Manifest is the on-disk shape of omarket.json: the fields a seller edits
// locally before pushing to the server with PUT /api/apps/{id}.
type Manifest struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Homepage      string `json:"homepage"`
	PriceUSDCents int    `json:"price_usd_cents"`
}

// Template placeholder values written by NewManifestTemplate. ManifestIssues
// flags any of these still present as not-yet-edited.
const (
	templateName            = "My App Name"
	templateDescriptionStub = "One line about"
	templateHomepage        = "https://example.com"
	templatePriceUSDCents   = 500
)

// NewManifestTemplate returns the starter omarket.json contents for a
// freshly claimed app id.
func NewManifestTemplate(id string) Manifest {
	return Manifest{
		ID:            id,
		Name:          templateName,
		Description:   templateDescriptionStub + " what your app does",
		Homepage:      templateHomepage,
		PriceUSDCents: templatePriceUSDCents,
	}
}

// ManifestIssues returns human-readable reasons m is not ready to push: any
// unedited template placeholder, or a non-positive price. A nil/empty
// result means m is ready to push.
func ManifestIssues(m Manifest) []string {
	var issues []string
	if m.Name == templateName {
		issues = append(issues, fmt.Sprintf("name is still the template value %q", templateName))
	}
	if strings.HasPrefix(m.Description, templateDescriptionStub) {
		issues = append(issues, fmt.Sprintf("description is still the template value (starts with %q)", templateDescriptionStub))
	}
	if m.Homepage == templateHomepage {
		issues = append(issues, fmt.Sprintf("homepage is still the template value %q", templateHomepage))
	}
	if m.PriceUSDCents <= 0 {
		issues = append(issues, "price_usd_cents must be greater than 0")
	}
	return issues
}

// WriteManifestTemplate writes a template omarket.json for id to path,
// refusing to overwrite an existing file.
func WriteManifestTemplate(path, id string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; remove it or edit it directly", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b, err := json.MarshalIndent(NewManifestTemplate(id), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// ReadManifest reads and parses the manifest at path.
func ReadManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return m, nil
}
