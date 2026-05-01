package subagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/genai-io/gen-code/internal/core"
	"github.com/genai-io/gen-code/internal/llm"
	"github.com/genai-io/gen-code/internal/log"
	"go.uber.org/zap"
)

type preparedRun struct {
	req              AgentRequest
	cfg              *runConfig
	cwd              string
	startedAt        time.Time
	hookID           string
	mu               sync.Mutex
	progress         []string
	cleanupWorkspace func()
}

func (r *preparedRun) close() {
	if r != nil && r.cleanupWorkspace != nil {
		r.cleanupWorkspace()
	}
}

func (r *preparedRun) sendProgress(msg string) {
	r.mu.Lock()
	r.progress = append(r.progress, msg)
	cb := r.req.OnProgress
	r.mu.Unlock()

	if cb != nil {
		cb(msg)
	}
}

func (r *preparedRun) progressSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.progress...)
}

func (e *Executor) prepareRun(req AgentRequest) (*preparedRun, error) {
	if err := e.validateRequest(req); err != nil {
		return nil, err
	}

	agentCwd, cleanupWorkspace, err := e.prepareWorkspace(req)
	if err != nil {
		return nil, err
	}

	cfg, err := e.prepareRunConfig(req)
	if err != nil {
		cleanupWorkspace()
		return nil, err
	}

	return &preparedRun{
		req:              req,
		cfg:              cfg,
		cwd:              agentCwd,
		startedAt:        time.Now(),
		hookID:           "a" + generateShortID(),
		progress:         make([]string, 0, 16),
		cleanupWorkspace: cleanupWorkspace,
	}, nil
}

func (e *Executor) attachRunContext(ctx context.Context, displayName string) context.Context {
	tracker := log.NewAgentTurnTracker(displayName, nil)
	return log.WithAgentTracker(ctx, tracker)
}

func (e *Executor) logRunStart(run *preparedRun) {
	log.Logger().Info("Starting agent execution",
		zap.String("agent", run.cfg.displayName),
		zap.String("description", run.req.Description),
		zap.Int("maxTurns", run.cfg.maxTurns),
	)
}

func (e *Executor) executePreparedRun(ctx context.Context, run *preparedRun) (*core.Result, error) {
	var onToolExec func(string, map[string]any)
	if run.req.OnProgress != nil {
		modelMsg := fmt.Sprintf("Model: %s", run.cfg.modelID)
		run.sendProgress(modelMsg)
		startMsg := fmt.Sprintf("Mode: %s · max %d turns", displayPermissionMode(run.cfg.permMode), run.cfg.maxTurns)
		run.sendProgress(startMsg)
		onToolExec = func(name string, params map[string]any) {
			msg := formatToolProgress(name, params)
			run.sendProgress(msg)
		}
	}
	ag, cleanupAgent, err := e.buildAgent(ctx, run.cfg, run.cwd, onToolExec)
	if err != nil {
		return nil, err
	}
	defer cleanupAgent()

	stopProgress := run.watchAgentProgress(ctx, ag.Outbox())
	defer stopProgress()

	if err := e.loadConversation(ag, ctx, run.req); err != nil {
		return nil, err
	}
	if run.req.OnProgress != nil {
		run.sendProgress("Thinking...")
	}

	result, err := ag.ThinkAct(ctx)
	if err != nil {
		if result != nil {
			return result, err
		}
		return nil, err
	}

	return result, nil
}

func (r *preparedRun) watchAgentProgress(ctx context.Context, outbox <-chan core.Event) func() {
	if r.req.OnProgress == nil || outbox == nil {
		return func() {}
	}

	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		var inputTokens, outputTokens int
		handle := func(ev core.Event) {
			if ev.Type != core.PostInfer {
				return
			}
			resp, ok := ev.Response()
			if !ok || resp == nil {
				return
			}
			inputTokens += resp.TokensIn
			outputTokens += resp.TokensOut
			if inputTokens > 0 || outputTokens > 0 {
				r.sendProgress(formatUsageProgress(inputTokens, outputTokens))
			}
		}
		drain := func() {
			for {
				select {
				case ev := <-outbox:
					handle(ev)
				default:
					return
				}
			}
		}
		for {
			select {
			case ev := <-outbox:
				handle(ev)
			case <-done:
				drain()
				return
			case <-ctx.Done():
				drain()
				return
			}
		}
	}()

	return func() {
		close(done)
		<-stopped
	}
}

func formatUsageProgress(inputTokens, outputTokens int) string {
	return fmt.Sprintf("Usage: input=%d output=%d", inputTokens, outputTokens)
}

func (e *Executor) logRunCompletion(run *preparedRun, result *core.Result, success bool) {
	logFields := []zap.Field{
		zap.String("agent", run.cfg.displayName),
		zap.String("stopReason", string(result.StopReason)),
		zap.Int("turns", result.Turns),
		zap.Int("inputTokens", result.TokensIn),
		zap.Int("outputTokens", result.TokensOut),
	}
	if success {
		log.Logger().Info("Agent completed", logFields...)
		return
	}
	log.Logger().Warn("Agent completed", logFields...)
}

func (e *Executor) buildAgentResult(run *preparedRun, result *core.Result) *AgentResult {
	success, errMsg := interpretStopReason(result, run.cfg.maxTurns)
	e.logRunCompletion(run, result, success)

	agentSessionID, agentTranscriptPath := e.persistSubagentSession(
		run.cfg.displayName,
		run.cfg.modelID,
		run.req.Description,
		result.Messages,
	)
	e.fireSubagentStop(run.req, run.hookID, agentSessionID, agentTranscriptPath, result.Content)

	return &AgentResult{
		AgentID:        agentSessionID,
		AgentName:      run.cfg.displayName,
		TranscriptPath: agentTranscriptPath,
		Model:          run.cfg.modelID,
		Success:        success,
		Content:        result.Content,
		Messages:       result.Messages,
		TurnCount:      result.Turns,
		ToolUses:       result.ToolUses,
		TokenUsage:     llm.TokenUsage{InputTokens: result.TokensIn, OutputTokens: result.TokensOut, TotalTokens: result.TokensIn + result.TokensOut},
		Duration:       time.Since(run.startedAt),
		Progress:       run.progressSnapshot(),
		Error:          errMsg,
	}
}

func (e *Executor) buildCancelledAgentResult(run *preparedRun, result *core.Result) *AgentResult {
	if result == nil || result.StopReason != core.StopCancelled {
		return nil
	}

	return &AgentResult{
		AgentName:  run.cfg.displayName,
		Model:      run.cfg.modelID,
		Success:    false,
		Content:    result.Content,
		Messages:   result.Messages,
		TurnCount:  result.Turns,
		ToolUses:   result.ToolUses,
		TokenUsage: llm.TokenUsage{InputTokens: result.TokensIn, OutputTokens: result.TokensOut, TotalTokens: result.TokensIn + result.TokensOut},
		Duration:   time.Since(run.startedAt),
		Progress:   run.progressSnapshot(),
		Error:      "agent cancelled",
	}
}
