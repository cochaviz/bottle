package local

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	models "cochaviz/mime/internal/models"
)

// LocalImageRepository persists sandbox image metadata in JSON files under BaseDir.
type LocalImageRepository struct {
	BaseDir string
}

// Save writes the sandbox image metadata to disk using its ID as the filename.
func (rep *LocalImageRepository) Save(image models.SandboxImage) error {
	if rep.BaseDir == "" {
		return errors.New("base directory is not configured")
	}
	if image.ID == "" {
		return errors.New("image id is required")
	}

	if err := os.MkdirAll(rep.BaseDir, 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(image, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(rep.BaseDir, image.ID+".json")
	return os.WriteFile(path, payload, 0o644)
}

// LatestForSpec returns the newest sandbox image for the provided spec id.
func (rep *LocalImageRepository) LatestForSpec(specID string) (*models.SandboxImage, error) {
	entries, err := os.ReadDir(rep.BaseDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var latest *models.SandboxImage
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		image, err := rep.loadImage(filepath.Join(rep.BaseDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if image == nil {
			continue
		}

		if image.Specification.ID != specID {
			continue
		}

		created := image.CreatedAt
		if latest == nil || created.After(latestTime) {
			clone := *image
			latest = &clone
			latestTime = created
		}
	}

	return latest, nil
}

// Get returns the sandbox image with the provided ID.
func (rep *LocalImageRepository) Get(imageID string) (*models.SandboxImage, error) {
	if imageID == "" {
		return nil, errors.New("image id is required")
	}
	return rep.loadImage(filepath.Join(rep.BaseDir, imageID+".json"))
}

func (rep *LocalImageRepository) loadImage(path string) (*models.SandboxImage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var image models.SandboxImage
	if err := json.Unmarshal(data, &image); err != nil {
		return nil, err
	}
	return &image, nil
}
