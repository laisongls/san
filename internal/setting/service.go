// Package setting owns the merged user+project settings and the
// central permission decision gate. Exposes *Manager directly.
package setting

import "sync"

// Options holds configuration for Initialize.
type Options struct {
	CWD string
}

// Initialize loads settings for cwd and installs the package-level *Manager.
func Initialize(opts Options) {
	s := InitForApp(opts.CWD)
	defaultManager = &Manager{settings: s}
}

// Default returns the package-level *Manager.
func Default() *Manager {
	return defaultManager
}

// DefaultIfInit returns the package-level *Manager, or nil if it
// has not been initialized with real settings yet.
func DefaultIfInit() *Manager {
	if defaultManager == nil || defaultManager.settings == nil {
		return nil
	}
	return defaultManager
}

// SetDefaultManager replaces the package-level *Manager. Intended for
// tests. A nil argument restores a fresh empty *Manager.
func SetDefaultManager(s *Manager) {
	if s == nil {
		defaultManager = &Manager{}
		return
	}
	defaultManager = s
}

// ResetDefaultManager restores a fresh empty *Manager. Intended for
// tests.
func ResetDefaultManager() {
	defaultManager = &Manager{}
}

var defaultManager = &Manager{}

// Manager wraps a *Settings under a mutex. Methods are mutex-protected
// views over the underlying settings.
type Manager struct {
	mu       sync.RWMutex
	settings *Settings
}

func (s *Manager) Snapshot() *Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings.Clone()
}

func (s *Manager) AllowBypass() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings != nil && s.settings.AllowBypass != nil && *s.settings.AllowBypass
}

func (s *Manager) IsGitRepo(cwd string) bool {
	return IsGitRepo(cwd)
}

func (s *Manager) Reload(cwd string) error {
	var (
		newSettings *Settings
		err         error
	)
	if cwd != "" {
		newSettings, err = LoadForCwd(cwd)
	} else {
		newSettings, err = Load()
	}
	if err != nil {
		return err
	}
	if newSettings == nil {
		newSettings = NewSettings()
	}
	mergeProviderPreferences(newSettings)
	cloned := newSettings.Clone()

	s.mu.Lock()
	s.settings = cloned
	s.mu.Unlock()

	return nil
}

func (s *Manager) DisabledTools() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil || s.settings.DisabledTools == nil {
		return make(map[string]bool)
	}
	result := make(map[string]bool, len(s.settings.DisabledTools))
	for k, v := range s.settings.DisabledTools {
		result[k] = v
	}
	return result
}

func (s *Manager) SearchProvider() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil {
		return ""
	}
	return s.settings.SearchProvider
}

func (s *Manager) SetSearchProvider(provider string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings != nil {
		s.settings.SearchProvider = provider
	}
}

func (s *Manager) Hooks() map[string][]Hook {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil {
		return nil
	}
	return s.settings.Hooks
}

func (s *Manager) CheckPermission(toolName string, args map[string]any, session *SessionPermissions) PermissionBehavior {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil {
		return Ask
	}
	return s.settings.CheckPermission(toolName, args, session)
}

func (s *Manager) HasPermissionToUseTool(toolName string, args map[string]any, session *SessionPermissions) PermissionDecision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil {
		return decide(Ask, "default: no settings loaded")
	}
	return s.settings.HasPermissionToUseTool(toolName, args, session)
}

func (s *Manager) ResolveHookAllow(toolName string, args map[string]any, session *SessionPermissions) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.settings == nil {
		return true
	}
	return s.settings.ResolveHookAllow(toolName, args, session)
}

func (s *Manager) GetDisabledToolsAt(userLevel bool) map[string]bool {
	return GetDisabledToolsAt(userLevel)
}

func (s *Manager) UpdateDisabledToolsAt(disabledTools map[string]bool, userLevel bool) error {
	return UpdateDisabledToolsAt(disabledTools, userLevel)
}
