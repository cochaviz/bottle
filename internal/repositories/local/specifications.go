package local

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	models "cochaviz/mime/internal/models"
)

// Repository using JSON files stored on disk for sandbox specifications.
type LocalSandboxSpecificationRepository struct {
	BaseDir string
}

// Get returns the latest stored specification for the provided id.
func (r *LocalSandboxSpecificationRepository) Get(specID string) (models.SandboxSpecification, error) {
	specs, err := r.ListVersions(specID)
	if err != nil {
		return models.SandboxSpecification{}, err
	}
	if len(specs) == 0 {
		return models.SandboxSpecification{}, errors.New("specification not found")
	}
	return specs[len(specs)-1], nil
}

// Save writes the specification as a new version.
func (r *LocalSandboxSpecificationRepository) Save(spec models.SandboxSpecification) (models.SandboxSpecification, error) {
	if spec.ID == "" {
		return models.SandboxSpecification{}, errors.New("spec id is required")
	}
	if spec.Version == "" {
		return models.SandboxSpecification{}, errors.New("spec version is required")
	}

	specDir := filepath.Join(r.BaseDir, spec.ID)
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		return models.SandboxSpecification{}, err
	}

	filename := filepath.Join(specDir, spec.Version+".json")
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return models.SandboxSpecification{}, err
	}

	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return models.SandboxSpecification{}, err
	}

	return spec, nil
}

// Update rewrites an existing specification version.
func (r *LocalSandboxSpecificationRepository) Update(spec models.SandboxSpecification) (models.SandboxSpecification, error) {
	return r.Save(spec)
}

// Delete removes all versions for the specification id.
func (r *LocalSandboxSpecificationRepository) Delete(specID string) error {
	if specID == "" {
		return errors.New("spec id is required")
	}

	dir := filepath.Join(r.BaseDir, specID)
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ListVersions returns every stored version for the specification id.
func (r *LocalSandboxSpecificationRepository) ListVersions(specID string) ([]models.SandboxSpecification, error) {
	dir := filepath.Join(r.BaseDir, specID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var specs []models.SandboxSpecification
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}

		var spec models.SandboxSpecification
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}

	sort.Slice(specs, func(i, j int) bool { return specs[i].Version < specs[j].Version })
	return specs, nil
}

// ListAll returns the most recent version for every specification id.
func (r *LocalSandboxSpecificationRepository) ListAll() ([]models.SandboxSpecification, error) {
	entries, err := os.ReadDir(r.BaseDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var specs []models.SandboxSpecification
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		versions, err := r.ListVersions(entry.Name())
		if err != nil {
			return nil, err
		}
		if len(versions) == 0 {
			continue
		}
		specs = append(specs, versions[len(versions)-1])
	}
	return specs, nil
}

// FilterByArchitecture returns specs matching the given architecture.
func (r *LocalSandboxSpecificationRepository) FilterByArchitecture(architecture string) ([]models.SandboxSpecification, error) {
	if architecture == "" {
		return r.ListAll()
	}

	specs, err := r.ListAll()
	if err != nil {
		return nil, err
	}

	var matches []models.SandboxSpecification
	for _, spec := range specs {
		if strings.EqualFold(spec.DomainProfile.Arch, architecture) {
			matches = append(matches, spec)
			continue
		}
		if spec.Metadata != nil {
			if arch, ok := spec.Metadata["arch"].(string); ok && strings.EqualFold(arch, architecture) {
				matches = append(matches, spec)
			}
		}
	}
	return matches, nil
}
