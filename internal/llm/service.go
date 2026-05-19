// Package llm holds the connected LLM provider plus the registry of
// available providers/models. Exposes *Hub directly. *Hub
// wraps the package-level *Setup (mutable provider/model/store under
// a mutex).
package llm

import "context"

// Hub is the concrete handle callers hold. Methods are
// mutex-protected views over the underlying *Setup.
type Hub struct {
	setup *Setup
}

// Options holds configuration for Initialize.
type Options struct{}

// Initialize discovers and connects to the best available LLM provider,
// then publishes the result as the package-level *Hub.
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

// Default returns the package-level *Hub.
func Default() *Hub {
	return defaultHub
}

// SetDefaultHub replaces the package-level *Hub. Intended for
// tests. A nil argument restores a fresh empty *Hub.
func SetDefaultHub(s *Hub) {
	if s == nil {
		defaultHub = &Hub{setup: &Setup{}}
		return
	}
	defaultHub = s
}

// ResetDefaultHub restores a fresh empty *Hub. Intended for
// tests.
func ResetDefaultHub() {
	defaultHub = &Hub{setup: &Setup{}}
}

var defaultHub = &Hub{setup: defaultSetup}

// --- methods (mutex-protected views over Setup) ---

func (s *Hub) Provider() Provider {
	s.setup.mu.RLock()
	defer s.setup.mu.RUnlock()
	return s.setup.Provider
}

func (s *Hub) SetProvider(p Provider) {
	s.setup.mu.Lock()
	defer s.setup.mu.Unlock()
	s.setup.Provider = p
}

func (s *Hub) ModelID() string { return s.setup.ModelID() }

func (s *Hub) CurrentModel() *CurrentModelInfo {
	s.setup.mu.RLock()
	defer s.setup.mu.RUnlock()
	return s.setup.CurrentModel
}

func (s *Hub) SetCurrentModel(info *CurrentModelInfo) {
	s.setup.mu.Lock()
	defer s.setup.mu.Unlock()
	s.setup.CurrentModel = info
}

func (s *Hub) Store() *Store {
	s.setup.mu.RLock()
	defer s.setup.mu.RUnlock()
	return s.setup.Store
}

func (s *Hub) NewClient(model string, maxTokens int) *Client {
	p := s.Provider()
	return NewClient(p, model, maxTokens)
}

func (s *Hub) ListProviders() map[Name][]Info {
	st := s.Store()
	return GetProvidersWithStatus(st)
}
