package models

import (
	"time"
)

// BuildStatus captures overall lifecycle states for an image build run.
type BuildStatus string

// Supported build statuses.
const (
	BuildStatusPending   BuildStatus = "pending"
	BuildStatusRunning   BuildStatus = "running"
	BuildStatusSucceeded BuildStatus = "succeeded"
	BuildStatusFailed    BuildStatus = "failed"
	BuildStatusCancelled BuildStatus = "cancelled"
)

// BuildContext provides the shared context passed across pipeline stages.
type BuildContext struct {
	Spec      SandboxSpecification
	Overrides map[string]any
}

// BuildProfile defines build-time options for the sandbox.
type BuildProfile struct {
	Console        string
	KernelURL      string
	InitrdURL      string
	Release        string
	DiskSizeGB     int
	PreseedEnabled bool
	MirrorHost     string
	MirrorPath     string
	NetworkName    string
}

// BuildRequest represents an enqueued request to build or rebuild an image.
type BuildRequest struct {
	SpecificationID string
	RequestedAt     time.Time
	Rebuild         bool
	Metadata        map[string]any

	ProfileOverrides map[string]any
}

// BuildOutput captures the result from the builder adapter before publication.
type BuildOutput struct {
	DiskImage          Artifact
	CompanionArtifacts []Artifact
	Metadata           map[string]any
}
