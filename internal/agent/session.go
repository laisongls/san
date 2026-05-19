package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/genai-io/gen-code/internal/core"
)

type Task struct {
	mu                 sync.RWMutex
	agent              core.Agent
	permBridge         *PermissionBridge
	cancel             context.CancelFunc
	pendingPermRequest *PermBridgeRequest
}

func (s *Task) Start(params BuildParams, messages []core.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.agent != nil {
		return fmt.Errorf("agent session already active")
	}

	ag, pb, err := buildAgent(params)
	if err != nil {
		return err
	}
	s.agent = ag
	s.permBridge = pb

	if len(messages) > 0 {
		s.agent.SetMessages(messages)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() { _ = s.agent.Run(ctx) }()

	return nil
}

func (s *Task) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Task) stopLocked() {
	if s.agent == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	select {
	case s.agent.Inbox() <- core.Message{Signal: core.SigStop}:
	default:
	}
	s.agent = nil
	s.permBridge = nil
	s.pendingPermRequest = nil
}

func (s *Task) Active() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agent != nil
}

func (s *Task) Send(content string, images []core.Image) {
	s.mu.RLock()
	ag := s.agent
	s.mu.RUnlock()
	if ag == nil {
		return
	}
	ag.Inbox() <- core.Message{Role: core.RoleUser, Content: content, Images: images}
}

func (s *Task) Outbox() <-chan core.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.agent == nil {
		return nil
	}
	return s.agent.Outbox()
}

func (s *Task) PermissionBridge() *PermissionBridge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.permBridge
}

func (s *Task) PendingPermission() *PermBridgeRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingPermRequest
}

func (s *Task) SetPendingPermission(req *PermBridgeRequest) {
	s.mu.Lock()
	s.pendingPermRequest = req
	s.mu.Unlock()
}

func (s *Task) System() core.System {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.agent == nil {
		return nil
	}
	return s.agent.System()
}
