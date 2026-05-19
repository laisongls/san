// Package task tracks background bash and subagent tasks for the TUI's
// task panel and the agent's TaskOutput / TaskList tools. Exposes
// *Tracker directly.
package task

// Options holds all dependencies for initialization.
type Options struct {
	OutputDir string
}

// Initialize creates the package-level *Tracker and configures it.
func Initialize(opts Options) {
	m := NewTracker()
	if opts.OutputDir != "" {
		m.SetOutputDir(opts.OutputDir)
	}
	defaultTracker = m
}

// Default returns the package-level *Tracker.
func Default() *Tracker {
	return defaultTracker
}

// SetDefaultTracker replaces the package-level *Tracker. Intended for
// tests. A nil argument restores a fresh empty *Tracker.
func SetDefaultTracker(m *Tracker) {
	if m == nil {
		defaultTracker = NewTracker()
		return
	}
	defaultTracker = m
}

// ResetDefaultTracker restores a fresh empty *Tracker. Intended for
// tests.
func ResetDefaultTracker() {
	defaultTracker = NewTracker()
}

var defaultTracker = NewTracker()

// SetOutputDir on *Tracker delegates to the package-level setOutputDir.
func (m *Tracker) SetOutputDir(dir string) error {
	return setOutputDir(dir)
}
