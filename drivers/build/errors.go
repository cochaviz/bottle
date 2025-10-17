package build

import "fmt"

// A buildError represents an error that occurred during the build process.
type BuildError struct {
	Message string
}

// Error returns the error message.
func (e *BuildError) Error() string {
	return fmt.Sprintf("%s", e.Message)
}
