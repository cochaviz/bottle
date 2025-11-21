package sandbox

import (
	"time"

	"github.com/cochaviz/bottle/arch"
	"github.com/cochaviz/bottle/internal/artifacts"
)

// BootMethod represents the supported boot mechanisms for a run profile.
type BootMethod string

// Supported boot methods.
const (
	BootMethodBIOS   BootMethod = "bios"
	BootMethodKernel BootMethod = "kernel"
)

// DomainProfile defines the VM hardware profile for building and running the sandbox.
type DomainProfile struct {
	Arch         arch.Architecture
	Machine      *string
	CPUModel     *string
	VCPUs        int
	RAMMB        int
	DiskBus      string
	DiskTarget   string
	CDBus        string
	CDPrefix     string
	SetupLetter  string
	SampleLetter string
	NetworkModel string
	ExtraArgs    []string
}

// RunProfile defines runtime options for the sandbox.
type RunProfile struct {
	RAMMB         int
	VCPUs         int
	BootMethod    BootMethod
	KernelPath    *string
	InitrdPath    *string
	KernelCmdline *string
	NetworkName   string
	NamePrefix    string
}

// SandboxSpec is a declarative description of a sandbox base image.
type SandboxSpecification struct {
	ID        string
	Version   string
	OSRelease string

	DomainProfile DomainProfile
	RunProfile    RunProfile

	Packages []string

	Hardening     map[string]any
	NetworkLayout map[string]any
	Metadata      map[string]any

	SetupFiles []artifacts.Artifact
}

// SandboxImage is a record describing a built sandbox image.
type SandboxImage struct {
	ID            string
	ImageArtifact artifacts.Artifact // Artifact representing the built sandbox image.
	CreatedAt     time.Time

	ReferenceSpecification SandboxSpecification // Specification used during the build process.
	CompanionArtifacts     []artifacts.Artifact // Artifacts beside the image produced during the build process and necessary for the sandbox to function.

	Metadata map[string]any
}

type SandboxState = string

const (
	SandboxPending SandboxState = "pending"
	SandboxRunning SandboxState = "running"
	SandboxPaused  SandboxState = "paused"
	SandboxStale   SandboxState = "stale"
	SandboxStopped SandboxState = "stopped"
)

type SandboxLeaseSpecification struct {
	DomainName string // Domain name for the sandbox.

	SampleDir string // Directory containing sample files to be mounted into the sandbox.

	SandboxImage          SandboxImage
	OverrideSpecification *SandboxSpecification // Specification used to override the image's specification (if provided)
}

type SandboxLease struct {
	ID        string
	StartTime time.Time
	EndTime   time.Time

	Specification SandboxLeaseSpecification
	SandboxState  SandboxState

	RunDir        string
	RuntimeConfig map[string]any
	Metadata      map[string]any
}

type SandboxCommand struct {
	Path    string
	Args    []string
	Timeout time.Duration
}

type SandboxCommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}
