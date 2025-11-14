package build

type BuildSpecificationRepository interface {
	Get(buildID string) (BuildSpecification, error)
	Save(spec BuildSpecification) (BuildSpecification, error)

	ListAll() ([]BuildSpecification, error)
	FilterByArchitecture(architecture string) ([]BuildSpecification, error)
}
