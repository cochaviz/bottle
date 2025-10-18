package local

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	models "cochaviz/mime/internal/models"
)

// LocalArtifactStore persists artifacts and metadata on disk under BaseDir.
type LocalArtifactStore struct {
	BaseDir string
}

// StoreArtifact copies the artifact into the repository directory and records metadata.
func (store *LocalArtifactStore) StoreArtifact(artifactPath string, kind models.ArtifactKind, metadata map[string]any) (models.Artifact, error) {
	if store.BaseDir == "" {
		return models.Artifact{}, errors.New("base directory is not configured")
	}

	if artifactPath == "" {
		return models.Artifact{}, errors.New("artifact path is required")
	}

	if err := os.MkdirAll(store.BaseDir, 0o755); err != nil {
		return models.Artifact{}, err
	}

	src, err := os.Open(artifactPath)
	if err != nil {
		return models.Artifact{}, err
	}
	defer src.Close()

	artifactID := uuid.NewString()
	ext := filepath.Ext(artifactPath)
	destName := artifactID
	if ext != "" {
		destName += ext
	}

	destPath := filepath.Join(store.BaseDir, destName)
	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return models.Artifact{}, err
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return models.Artifact{}, err
	}
	if err := dst.Close(); err != nil {
		return models.Artifact{}, err
	}

	artifact := models.Artifact{
		ID:          artifactID,
		Kind:        kind,
		URI:         fileURI(destPath),
		Metadata:    cloneMetadata(metadata),
		ContentType: detectContentType(destPath),
	}

	if err := store.writeMetadata(destPath, artifact); err != nil {
		return models.Artifact{}, err
	}

	return artifact, nil
}

// RemoveArtifact deletes the artifact file and its metadata document.
func (store *LocalArtifactStore) RemoveArtifact(artifact models.Artifact) error {
	path, err := pathFromFileURI(artifact.URI)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	metaPath := metadataPath(path)
	if err := os.Remove(metaPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}

// Clear removes all artifacts and metadata under the store's base directory.
func (store *LocalArtifactStore) Clear() error {
	entries, err := os.ReadDir(store.BaseDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(store.BaseDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (store *LocalArtifactStore) writeMetadata(filePath string, artifact models.Artifact) error {
	payload, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath(filePath), payload, 0o644)
}

func metadataPath(path string) string {
	return path + ".json"
}

func fileURI(path string) string {
	return "file://" + path
}

func pathFromFileURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "file://") {
		return "", errors.New("unsupported URI scheme")
	}
	return strings.TrimPrefix(uri, "file://"), nil
}

func detectContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gz":
		return "application/gzip"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for k, v := range metadata {
		cloned[k] = v
	}
	return cloned
}
