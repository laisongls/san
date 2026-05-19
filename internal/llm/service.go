// Package llm holds the connected LLM provider plus the registry of
// available providers/models. Exposes *ClientFactory directly. *ClientFactory
// wraps the package-level *Setup (mutable provider/model/store under
// a mutex).
package llm

import "context"

// ClientFactory is the concrete handle callers hold. Methods are
// mutex-protected views over the underlying *Setup.
type ClientFactory struct {
	setup *Setup
}

// Options holds configuration for Initialize.
type Options struct{}

// Initialize discovers and connects to the best available LLM provider,
// then publishes the result as the package-level *ClientFactory.
func Initialize(opts Options) {
	store, _ := NewStore()
	if store == nil {
		return
	}

	defaultSetup.mu.Lock()
	defaultSetup.Store = store
	defaultSetup.CurrentModel = store.GetCurrentModel()
	defaultSetup.mu.Unlock()

	ctx := context.Background()

	defaultSetup.mu.RLock()
	cm := defaultSetup.CurrentModel
	defaultSetup.mu.RUnlock()

	if cm != nil {
		if p, err := GetProvider(ctx, cm.Provider, cm.AuthMethod); err == nil {
			defaultSetup.mu.Lock()
			defaultSetup.Provider = p
			defaultSetup.mu.Unlock()
			setSingleton()
			return
		}
	}

	for providerName, conn := range store.GetConnections() {
		if p, err := GetProvider(ctx, Name(providerName), conn.AuthMethod); err == nil {
			defaultSetup.mu.Lock()
			defaultSetup.Provider = p
			defaultSetup.mu.Unlock()
			setSingleton()
			return
		}
	}

	setSingleton()
}

// Default returns the package-level *ClientFactory.
func Default() *ClientFactory {
	return defaultClientFactory
}

// SetDefaultClientFactory replaces the package-level *ClientFactory. Intended for
// tests. A nil argument restores a fresh empty *ClientFactory.
func SetDefaultClientFactory(s *ClientFactory) {
	if s == nil {
		defaultClientFactory = &ClientFactory{setup: &Setup{}}
		return
	}
	defaultClientFactory = s
}

// ResetDefaultClientFactory restores a fresh empty *ClientFactory. Intended for
// tests.
func ResetDefaultClientFactory() {
	defaultClientFactory = &ClientFactory{setup: &Setup{}}
}

var defaultClientFactory = &ClientFactory{setup: defaultSetup}

// --- methods (mutex-protected views over Setup) ---

func (s *ClientFactory) Provider() Provider {
	s.setup.mu.RLock()
	defer s.setup.mu.RUnlock()
	return s.setup.Provider
}

func (s *ClientFactory) SetProvider(p Provider) {
	s.setup.mu.Lock()
	defer s.setup.mu.Unlock()
	s.setup.Provider = p
}

func (s *ClientFactory) ModelID() string { return s.setup.ModelID() }

func (s *ClientFactory) CurrentModel() *CurrentModelInfo {
	s.setup.mu.RLock()
	defer s.setup.mu.RUnlock()
	return s.setup.CurrentModel
}

func (s *ClientFactory) SetCurrentModel(info *CurrentModelInfo) {
	s.setup.mu.Lock()
	defer s.setup.mu.Unlock()
	s.setup.CurrentModel = info
}

func (s *ClientFactory) Store() *Store {
	s.setup.mu.RLock()
	defer s.setup.mu.RUnlock()
	return s.setup.Store
}

func (s *ClientFactory) NewClient(model string, maxTokens int) *Client {
	p := s.Provider()
	return NewClient(p, model, maxTokens)
}

func (s *ClientFactory) ListProviders() map[Name][]Info {
	st := s.Store()
	return GetProvidersWithStatus(st)
}
