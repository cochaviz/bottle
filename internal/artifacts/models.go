package artifacts

type ArtifactKind string

const (
	ImageArtifact  ArtifactKind = "image"  // Artifact for sandbox images
	BuildArtifact  ArtifactKind = "build"  // Artifact for build artifacts
	BinaryArtifact ArtifactKind = "binary" // Artifact for binary files
	TextArtifact   ArtifactKind = "text"   // Artifact for generic text artifacts
)

type Artifact struct {
	ID   string
	Kind ArtifactKind
	URI  string

	Checksum    *string
	ContentType string
	Metadata    map[string]any
}
