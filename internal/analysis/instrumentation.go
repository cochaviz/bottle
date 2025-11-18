package analysis

import (
	"cochaviz/bottle/internal/sandbox"
	"context"
)

type InstrumentationVariableName = string

const (
	InstrumentationC2Address   = "C2Ip"
	InstrumentationVMIP        = "VmIp"
	InstrumentationVMInterface = "VmInterface"
	InstrumentationSampleName  = "SampleName"
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
