// Package cron schedules recurring and one-shot jobs that fire user
// prompts back into the agent loop. Exposes *Scheduler directly.
package cron

// Options configures the package-level *Scheduler.
type Options struct {
	StoragePath string // file path for durable job persistence
}

// Initialize creates and configures the package-level *Scheduler.
func Initialize(opts Options) {
	s := NewScheduler()
	if opts.StoragePath != "" {
		s.SetStoragePath(opts.StoragePath)
	}
	defaultScheduler = s
}

// Default returns the package-level *Scheduler.
func Default() *Scheduler {
	return defaultScheduler
}

// SetDefaultScheduler replaces the package-level *Scheduler. Intended for tests.
// A nil argument restores a fresh empty *Scheduler.
func SetDefaultScheduler(s *Scheduler) {
	if s == nil {
		defaultScheduler = NewScheduler()
		return
	}
	defaultScheduler = s
}

// ResetDefaultScheduler restores a fresh empty *Scheduler. Intended for tests.
func ResetDefaultScheduler() {
	defaultScheduler = NewScheduler()
}

var defaultScheduler = NewScheduler()
