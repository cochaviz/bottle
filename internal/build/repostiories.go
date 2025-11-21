package build

import "github.com/cochaviz/bottle/arch"

type BuildSpecificationRepository interface {
	Get(buildID string) (BuildSpecification, error)
	Save(spec BuildSpecification) (BuildSpecification, error)

	ListAll() ([]BuildSpecification, error)
	FilterByArchitecture(architecture arch.Architecture) ([]BuildSpecification, error)
}
