package sandbox

import "github.com/cochaviz/bottle/arch"

// ImageRepository persists metadata for built sandbox images.
type ImageRepository interface {
	Save(image SandboxImage) error
	LatestForSpec(specID string) (*SandboxImage, error)
	FilterByArchitecture(architecture arch.Architecture) ([]*SandboxImage, error)
	Get(imageID string) (*SandboxImage, error)
	ListForSpec(specID string) ([]*SandboxImage, error)
	Delete(imageID string) error
}
