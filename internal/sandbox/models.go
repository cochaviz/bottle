package sandbox

import (
	"cochaviz/mime/internal/artifacts"
	"time"
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
	Arch         string
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
	ID            string
	Version       string
	OSRelease     string
	DomainProfile DomainProfile
	RunProfile    RunProfile
	Packages      []string
	Hardening     map[string]any
	NetworkLayout map[string]any
	Metadata      map[string]any
}

// SandboxImage is a record describing a built sandbox image.
type SandboxImage struct {
	ID            string
	Specification SandboxSpecification
	Image         artifacts.Artifact // Artifact representing the built sandbox image.
	CreatedAt     time.Time

	Metadata           map[string]any
	CompanionArtifacts []artifacts.Artifact // Artifacts beside the image produced during the build process and necessary for the sandbox to function.
}

// StaticAnalysis represents the static analysis of a system.
type StaticAnalysis struct {
	// ...
}

// DynamicAnalysis represents the dynamic analysis of a system.
type DynamicAnalysis struct {
	// ...
}

type Sample struct {
	ID       string
	Name     string
	Artifact string
}

type SandboxLeaseState = string

type SandboxLeaseSpecification struct {
	SandboxSpecification SandboxSpecification
	SandboxImage         SandboxImage

	ttl time.Duration // Duration for which the lease is valid.
}

type SandboxLease struct {
	ID        int64
	StartTime time.Time
	EndTime   time.Time

	Specification SandboxLeaseSpecification
	State         SandboxLeaseState

	RuntimeConfig map[string]any
	Metadata      map[string]any
}
