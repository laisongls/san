// Package agent owns the foreground agent session lifecycle. *Task
// is the concrete handle; the package exposes it directly.
package agent

// Options holds dependencies for initialization.
type Options struct{}

// Initialize installs a fresh *Task as the package-level default.
func Initialize(opts Options) {
	defaultTask = &Task{}
}

// Default returns the package-level *Task.
func Default() *Task {
	return defaultTask
}

// SetDefaultTask replaces the package-level *Task. Intended for
// tests. A nil argument restores a fresh empty *Task.
func SetDefaultTask(s *Task) {
	if s == nil {
		defaultTask = &Task{}
		return
	}
	defaultTask = s
}

// ResetDefaultTask restores a fresh empty *Task. Intended for
// tests.
func ResetDefaultTask() {
	defaultTask = &Task{}
}

var defaultTask = &Task{}
