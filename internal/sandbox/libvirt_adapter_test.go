package sandbox

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cochaviz/mime/internal/artifacts"

	"github.com/kdomanski/iso9660"
)

func TestLibvirtDriverAcquireCreatesWorkspace(t *testing.T) {
	stubQemuImg(t)

	baseImage := filepath.Join(t.TempDir(), "base.qcow2")
	if err := os.WriteFile(baseImage, []byte("base-image"), 0o644); err != nil {
		t.Fatalf("write base image: %v", err)
	}

	driver := &LibvirtDriver{
		ConnectionURI: "qemu:///system",
		BaseDir:       filepath.Join(t.TempDir(), "runs"),
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	spec := testLeaseSpecification(baseImage)
	spec.DomainName = "sample-domain"

	lease, err := driver.Acquire(spec)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	wantRunDir := filepath.Join(driver.BaseDir, spec.DomainName)
	if lease.RunDir != wantRunDir {
		t.Fatalf("RunDir = %q, want %q", lease.RunDir, wantRunDir)
	}
	if _, err := os.Stat(lease.RunDir); err != nil {
		t.Fatalf("stat run dir: %v", err)
	}
	if lease.SandboxState != SandboxPending {
		t.Fatalf("SandboxState = %q, want %q", lease.SandboxState, SandboxPending)
	}
	if lease.ID != spec.DomainName {
		t.Fatalf("lease ID = %q, want %q", lease.ID, spec.DomainName)
	}
	if !lease.StartTime.IsZero() {
		t.Fatalf("StartTime should be zero before start")
	}

	domainXMLPath := runtimePath(t, lease.RuntimeConfig, "domain_xml")
	domainXML, err := os.ReadFile(domainXMLPath)
	if err != nil {
		t.Fatalf("read domain xml: %v", err)
	}
	if !strings.Contains(string(domainXML), "<name>sample-domain</name>") {
		t.Fatalf("domain XML does not contain expected domain name: %s", domainXML)
	}

	overlayPath := runtimePath(t, lease.RuntimeConfig, "overlay_path")
	if _, err := os.Stat(overlayPath); err != nil {
		t.Fatalf("stat overlay: %v", err)
	}
	if !strings.Contains(string(domainXML), fmt.Sprintf("<source file='%s'/>", overlayPath)) {
		t.Fatalf("domain XML missing overlay reference: %s", domainXML)
	}

	basePath := runtimePath(t, lease.RuntimeConfig, "base_image")
	if basePath != baseImage {
		t.Fatalf("runtime base image = %q, want %q", basePath, baseImage)
	}

	if uri := runtimeValue(t, lease.RuntimeConfig, "connection_uri"); uri != driver.ConnectionURI {
		t.Fatalf("runtime connection uri = %q, want %q", uri, driver.ConnectionURI)
	}

	if got := lease.Metadata["driver"]; got != "libvirt" {
		t.Fatalf("metadata driver = %v, want %q", got, "libvirt")
	}
	if got := lease.Metadata["domain_name"]; got != spec.DomainName {
		t.Fatalf("metadata domain_name = %v, want %q", got, spec.DomainName)
	}
	if got := lease.Metadata["image_id"]; got != spec.SandboxImage.ID {
		t.Fatalf("metadata image_id = %v, want %q", got, spec.SandboxImage.ID)
	}
}

func TestLibvirtDriverAcquireValidation(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	specWithImage := SandboxLeaseSpecification{
		SandboxImage: SandboxImage{
			ImageArtifact: artifacts.Artifact{URI: "file:///tmp/fake.qcow2"},
			ReferenceSpecification: SandboxSpecification{
				DomainProfile: DomainProfile{RAMMB: 1024, VCPUs: 1, DiskBus: "virtio", DiskTarget: "vda"},
				RunProfile:    RunProfile{RAMMB: 1024, VCPUs: 1},
			},
		},
	}

	testCases := []struct {
		name    string
		driver  *LibvirtDriver
		spec    SandboxLeaseSpecification
		wantErr string
	}{
		{
			name: "override specification unsupported",
			driver: &LibvirtDriver{
				BaseDir:       baseDir,
				ConnectionURI: "qemu:///system",
			},
			spec: SandboxLeaseSpecification{
				OverrideSpecification: &SandboxSpecification{},
			},
			wantErr: "override specification",
		},
		{
			name: "missing base directory",
			driver: &LibvirtDriver{
				ConnectionURI: "qemu:///system",
			},
			spec:    specWithImage,
			wantErr: "BaseDir",
		},
		{
			name: "missing connection uri",
			driver: &LibvirtDriver{
				BaseDir: baseDir,
			},
			spec:    specWithImage,
			wantErr: "ConnectionURI",
		},
		{
			name: "missing image artifact uri",
			driver: &LibvirtDriver{
				BaseDir:       baseDir,
				ConnectionURI: "qemu:///system",
			},
			spec:    SandboxLeaseSpecification{},
			wantErr: "image artifact URI",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := tc.driver.Acquire(tc.spec)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Acquire() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestLibvirtDriverAcquireMountsSampleAndSetupDirs(t *testing.T) {
	stubQemuImg(t)

	baseImage := filepath.Join(t.TempDir(), "base.qcow2")
	if err := os.WriteFile(baseImage, []byte("base-image"), 0o644); err != nil {
		t.Fatalf("write base image: %v", err)
	}

	driver := &LibvirtDriver{
		ConnectionURI: "qemu:///session",
		BaseDir:       filepath.Join(t.TempDir(), "runs"),
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	sampleDir := t.TempDir()
	sampleFile := filepath.Join(sampleDir, "sample.bin")
	if err := os.WriteFile(sampleFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("write sample file: %v", err)
	}

	setupDir := t.TempDir()
	setupScript := filepath.Join(setupDir, "setup.ps1")
	if err := os.WriteFile(setupScript, []byte("Write-Host setup"), 0o644); err != nil {
		t.Fatalf("write setup script: %v", err)
	}

	spec := testLeaseSpecification(baseImage)
	spec.SampleDir = sampleDir
	spec.SetupDir = setupDir

	lease, err := driver.Acquire(spec)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	setupPath := filepath.Join(lease.RunDir, "setup.iso")
	samplePath := filepath.Join(lease.RunDir, "sample.iso")

	if _, err := os.Stat(setupPath); err != nil {
		t.Fatalf("stat setup disk: %v", err)
	}
	if _, err := os.Stat(samplePath); err != nil {
		t.Fatalf("stat sample disk: %v", err)
	}

	if _, err := os.Stat(filepath.Join(setupDir, "setup")); err == nil || !os.IsNotExist(err) {
		t.Fatalf("setup marker should not exist in original directory: %v", err)
	}

	domainXMLPath := runtimePath(t, lease.RuntimeConfig, "domain_xml")
	domainXML, err := os.ReadFile(domainXMLPath)
	if err != nil {
		t.Fatalf("read domain xml: %v", err)
	}
	if !strings.Contains(string(domainXML), setupPath) {
		t.Fatalf("domain XML missing setup disk reference: %s", domainXML)
	}
	if !strings.Contains(string(domainXML), samplePath) {
		t.Fatalf("domain XML missing sample disk reference: %s", domainXML)
	}

	if !isoContainsFile(t, setupPath, "setup") {
		t.Fatalf("setup disk missing setup marker")
	}
	if !isoContainsFile(t, samplePath, "sample.bin") {
		t.Fatalf("sample disk missing sample payload")
	}
}

func testLeaseSpecification(imagePath string) SandboxLeaseSpecification {
	spec := SandboxLeaseSpecification{
		SandboxImage: SandboxImage{
			ID: "image-123",
			ImageArtifact: artifacts.Artifact{
				URI: fmt.Sprintf("file://%s", imagePath),
			},
			ReferenceSpecification: SandboxSpecification{
				DomainProfile: DomainProfile{
					Arch:         "x86_64",
					VCPUs:        2,
					RAMMB:        2048,
					DiskBus:      "virtio",
					DiskTarget:   "vda",
					CDBus:        "sata",
					NetworkModel: "virtio",
				},
				RunProfile: RunProfile{
					RAMMB:       1024,
					VCPUs:       1,
					NetworkName: "lab",
					NamePrefix:  "sandbox",
				},
			},
		},
	}

	return spec
}

func stubQemuImg(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	stubPath := filepath.Join(dir, "qemu-img")
	script := `#!/bin/sh
overlay=""
for arg in "$@"; do
  overlay="$arg"
done
if [ -z "$overlay" ]; then
  echo "missing overlay argument" >&2
  exit 1
fi
>"$overlay"
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub qemu-img: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func runtimePath(t *testing.T, cfg map[string]any, key string) string {
	t.Helper()

	value, ok := cfg[key]
	if !ok {
		t.Fatalf("runtime config missing %s", key)
	}
	path, ok := value.(string)
	if !ok {
		t.Fatalf("runtime config %s is %T, want string", key, value)
	}
	return path
}

func runtimeValue(t *testing.T, cfg map[string]any, key string) string {
	t.Helper()
	val, ok := cfg[key]
	if !ok {
		t.Fatalf("runtime config missing %s", key)
	}
	text, ok := val.(string)
	if !ok {
		t.Fatalf("runtime config %s is %T, want string", key, val)
	}
	return text
}

func isoContainsFile(t *testing.T, isoPath, fileName string) bool {
	t.Helper()

	f, err := os.Open(isoPath)
	if err != nil {
		t.Fatalf("open iso file: %v", err)
	}
	defer f.Close()

	image, err := iso9660.OpenImage(f)
	if err != nil {
		t.Fatalf("open iso image: %v", err)
	}

	root, err := image.RootDir()
	if err != nil {
		t.Fatalf("get iso root: %v", err)
	}
	return isoSearchFile(root, fileName)
}

func isoSearchFile(entry *iso9660.File, want string) bool {
	if entry == nil {
		return false
	}
	if !entry.IsDir() && strings.EqualFold(entry.Name(), want) {
		return true
	}
	if !entry.IsDir() {
		return false
	}

	children, err := entry.GetChildren()
	if err != nil {
		return false
	}
	for _, child := range children {
		if isoSearchFile(child, want) {
			return true
		}
	}
	return false
}
