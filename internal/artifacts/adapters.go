package artifacts

// ArtifactStore stores various artifacts.
type ArtifactStore interface {
	StoreArtifact(artifactPath string, kind ArtifactKind, metadata map[string]any) (Artifact, error)
	RemoveArtifact(artifact Artifact) error
	Clear() error
}
