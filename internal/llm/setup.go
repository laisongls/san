package llm

import "sync"

// defaultSetup is the package-level LLM setup, initialized by Initialize().
// External callers should use Default() to get the *ClientFactory singleton.
var defaultSetup = &Setup{}

// Setup holds the initialized LLM provider state needed by the app layer.
type Setup struct {
	mu           sync.RWMutex
	Store        *Store
	Provider     Provider
	CurrentModel *CurrentModelInfo
}

// setSingleton publishes defaultSetup as the package-level *ClientFactory.
func setSingleton() {
	defaultClientFactory = &ClientFactory{setup: defaultSetup}
}

// ModelID returns the current model ID, or empty string if none.
func (s *Setup) ModelID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.CurrentModel != nil {
		return s.CurrentModel.ModelID
	}
	return ""
}
