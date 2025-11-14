package sandbox

import (
	"cochaviz/mime/internal/artifacts"
)

type Planner struct {
}

func NewPlanner(artifactStore *artifacts.ArtifactStore) *Planner {
	return &Planner{}
}

// Proposes architecture based on the static and dynamic analysis
func (p *Planner) ProposeArchitecture(sa *StaticAnalysis) (string, error) {
	return "", nil
}

// Proposes C2 addresses based on the dynamic analysis
func (p *Planner) ProposeC2Addresses(da *DynamicAnalysis) (*[]string, error) {
	return nil, nil
}
