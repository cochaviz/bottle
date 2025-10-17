package build

import (
	models "cochaviz/mime/models"
)

// BuildEnvironmentPreparer provisions and cleans up the build environment.
type BuildEnvironmentPreparer interface {
	Prepare(context models.BuildContext) (BuildEnvironment, error)
}

type BuildEnvironment interface {
	Cleanup(context models.BuildContext) error
}

// BuildDriver drives the build workflow to produce an image.
type BuildDriver interface {
	Build(context models.BuildContext, environment BuildEnvironment) (models.BuildOutput, error)
}
