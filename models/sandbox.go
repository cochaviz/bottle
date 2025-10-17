package models

import "time"

// BootMethod represents the supported boot mechanisms for a run profile.
type BootMethod string

// Supported boot methods.
const (
	BootMethodBIOS   BootMethod = "bios"
	BootMethodKernel BootMethod = "kernel"
)

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
	ID          string
	Version         string
	OSRelease       string
	DomainProfile   DomainProfile
	BuildProfile    BuildProfile
	RunProfile      RunProfile
	Packages        []string
	Hardening       map[string]any
	NetworkLayout   map[string]any
	InstallerAssets map[string]string
	Metadata        map[string]any
}

// SandboxImage is a record describing a built sandbox image.
type SandboxImage struct {
	ID            string
	Specification SandboxSpecification
	Image         Artifact // Artifact representing the built sandbox image.
	CreatedAt     time.Time

	Metadata           map[string]any
	CompanionArtifacts []Artifact // Artifacts beside the image produced during the build process and necessary for the sandbox to function.
}
