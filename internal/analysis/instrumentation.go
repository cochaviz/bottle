package analysis

import (
	"cochaviz/mime/internal/sandbox"
	"context"
)

type InstrumentationVariableName = string

const (
	InstrumentationC2Address   = "c2_address"
	InstrumentationVMIP        = "vm_ip"
	InstrumentationVMInterface = "vm_interface"
	InstrumentationSampleName  = "sample_name"
)

type InstrumentationVariable struct {
	Name  InstrumentationVariableName
	Value string
}

type Instrumentation interface {
	// Start starts the instrumentation with the given variables.
	Start(ctx context.Context, lease sandbox.SandboxLease, variables ...InstrumentationVariable) error

	// Close closes the instrumentation.
	Close() error
}
