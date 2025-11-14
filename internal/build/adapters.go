package build

import "context"

// BuildEnvironmentPreparer provisions and cleans up the build environment.
type BuildEnvironmentPreparer interface {
	Prepare(context BuildContext) (BuildEnvironment, error)
}

type BuildEnvironment interface {
	Cleanup(context BuildContext) error
}

// BuildDriver drives the build workflow to produce an image.
type BuildDriver interface {
	Build(ctx context.Context, buildContext BuildContext, environment BuildEnvironment) (BuildOutput, error)
}
