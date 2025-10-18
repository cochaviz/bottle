package setup

import "log/slog"

var packageLogger *slog.Logger = slog.Default()

// SetLogger configures the package logger used for setup operations.
func SetLogger(logger *slog.Logger) {
	if logger == nil {
		packageLogger = slog.Default()
		return
	}
	packageLogger = logger
}

func getLogger() *slog.Logger {
	if packageLogger != nil {
		return packageLogger
	}
	return slog.Default()
}
