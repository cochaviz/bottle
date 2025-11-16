package sandbox

// ImageRepository persists metadata for built sandbox images.
type ImageRepository interface {
	Save(image SandboxImage) error
	LatestForSpec(specID string) (*SandboxImage, error)
	FilterByArchitecture(architecture string) ([]*SandboxImage, error)
	Get(imageID string) (*SandboxImage, error)
}
