// Submit dispatch — what happens when the user presses Enter.
//
// The whole path lives in this file. Every step is annotated with its
// numbered position in the flow so a reader can follow Enter → agent
// without jumping packages.
package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/genai-io/gen-code/internal/app/input"
	"github.com/genai-io/gen-code/internal/core"
	"github.com/genai-io/gen-code/internal/log"
)

// handleSubmit is the Enter handler. The dispatch follows this shape:
//
//	Step 1: read textarea, early-exit on empty
//	Step 2: if a turn is already streaming, queue this input and stop
//	Step 3: otherwise hand off to dispatchSubmission for the normal path
func (m *model) handleSubmit() tea.Cmd {
	m.userInput.PromptSuggestion.Clear()

	// Step 1: read raw input.
	raw := strings.TrimSpace(m.userInput.FullValue())
	if raw == "" && len(m.userInput.Images.Pending) == 0 {
		return nil
	}

	// Step 2: if a turn is currently streaming, park the input in the
	// queue and wait. The queue is drained at turn-boundary or after
	// stream cancel.
	if m.conv.Stream.Active {
		log.QueueLog("handleSubmit: stream active, enqueue %q", raw)
		return m.enqueueWhileStreaming(raw)
	}

	// Step 3: stream is idle — run the full submission pipeline.
	log.QueueLog("handleSubmit: stream idle, normal submit %q", raw)
	m.conv.Compact.ClearResult()
	return m.dispatchSubmission(raw)
}

// enqueueWhileStreaming parks the input in the queue and resets the
// textarea. The queued item is dequeued either by drainInputQueueAfterCancel
// (on stream cancel) or by drainTurnQueues (at turn boundary).
func (m *model) enqueueWhileStreaming(raw string) tea.Cmd {
	images := m.userInput.PendingImages()
	if m.userInput.Queue.Enqueue(raw, images) < 0 {
		m.conv.AddNotice("Input queue is full. Please wait for the current turn to complete.")
		return nil
	}
	m.userInput.Reset()
	log.QueueLog("enqueueWhileStreaming: queued %q queueLen=%d", raw, m.userInput.Queue.Len())
	return nil
}

// dispatchSubmission runs the actual submission pipeline. Shared by the
// Enter handler (handleSubmit) and the queue drain that runs after
// stream cancel (drainInputQueueAfterCancel).
//
//	Step 1: "exit" literal → quit
//	Step 2: prompt hook gate → block with notice if rejected
//	Step 3: record to input history before further branching
//	Step 4: slash command? hand to the slash controller (manages its own
//	        textarea + conv state)
//	Step 5: send to agent — build user message (with image refs), append
//	        to conv, reset textarea, hand off to SubmitToAgent
func (m *model) dispatchSubmission(raw string) tea.Cmd {
	// Step 1: exit literal.
	if input.IsExitRequest(raw) {
		cmd, _ := m.QuitWithCancel()
		return cmd
	}

	// Step 2: prompt hook gate.
	if blocked, reason := m.checkPromptHook(context.Background(), raw); blocked {
		m.conv.Append(core.ChatMessage{Role: core.RoleNotice, Content: "Prompt blocked: " + reason})
		m.userInput.Reset()
		return tea.Batch(m.CommitMessages()...)
	}

	// Step 3: history.
	m.userInput.RecordSubmission(m.env.CWD, raw)

	// Step 4: slash command — controller owns its own state changes.
	if cmd, handled := m.runSlashCommandIfMatched(raw); handled {
		return cmd
	}

	// Step 5: send to agent (no plugin scope — user-typed prompts run
	// outside any plugin context). The previous turn's plugin root, if
	// any, was already cleared by OnTurnEnd.
	msg, ok := m.buildUserMessage(raw)
	if !ok {
		// image error notice already appended by buildUserMessage
		return tea.Batch(m.CommitMessages()...)
	}
	m.conv.Append(msg)
	m.userInput.Reset()
	return m.SubmitToAgent(msg.Content, msg.Images)
}

// runSlashCommandIfMatched returns (cmd, true) if `raw` is a slash command
// the controller handled, or (nil, false) if it should fall through to a
// provider turn.
func (m *model) runSlashCommandIfMatched(raw string) (tea.Cmd, bool) {
	ctrl := input.NewSlashCommandController(m.slashCommandEnv())
	return ctrl.HandleSubmit(raw)
}

// buildUserMessage resolves image references in raw text into a ChatMessage
// ready to append. Returns ok=false if image resolution failed (in which
// case a notice has already been appended to conv).
func (m *model) buildUserMessage(raw string) (core.ChatMessage, bool) {
	content, fileImages, err := input.ProcessImageRefs(m.env.CWD, raw)
	if err != nil {
		m.conv.Append(core.ChatMessage{Role: core.RoleNotice, Content: "Image error: " + err.Error()})
		return core.ChatMessage{}, false
	}
	displayContent := content
	content, inlineImages := m.userInput.ExtractInlineImages(content)
	allImages := make([]core.Image, 0, len(inlineImages)+len(fileImages))
	allImages = append(allImages, inlineImages...)
	allImages = append(allImages, fileImages...)
	return core.ChatMessage{
		Role:           core.RoleUser,
		Content:        content,
		DisplayContent: displayContent,
		Images:         allImages,
	}, true
}

// drainInputQueueAfterCancel pops one queued item (if any) and runs it
// through the normal submission pipeline. Called from handleStreamCancel
// so a user who queued input while a turn was streaming sees the next one
// run after Ctrl+C.
func (m *model) drainInputQueueAfterCancel() tea.Cmd {
	item, ok := m.userInput.Queue.Dequeue()
	if !ok {
		return nil
	}
	m.conv.Compact.ClearResult()
	m.userInput.RestoreImages(item.Images)
	return m.dispatchSubmission(item.Content)
}

// SubmitToAgent is the single exit point for every "send this content to
// the agent" path — user Enter, slash command output, skill button, cron
// fire, hook continuation, hub notification. It ensures the agent session
// is up, hands `content` + `images` to the agent's inbox, and returns a
// cmd that polls the outbox. Agent events flow back through Update →
// routeToSubModel → conv.Update → model conv.Runtime callbacks
// (see model_agent_events.go).
//
// On error (no provider connected, ensureAgentSession failed), appends a
// notice to conv and returns a commit cmd — the agent is not contacted.
func (m *model) SubmitToAgent(content string, images []core.Image) tea.Cmd {
	log.QueueLog("SubmitToAgent: %q", truncate(content, 60))
	if m.env.LLMProvider == nil {
		return m.notifyNoProvider()
	}

	startCmd, err := m.ensureAgentSession(content)
	if err != nil {
		m.conv.AddNotice("Failed to start agent: " + err.Error())
		return tea.Batch(m.CommitMessages()...)
	}

	m.env.DetectThinkingKeywords(content)

	sendCmd := m.sendToAgent(content, images)
	if startCmd != nil {
		return tea.Batch(startCmd, sendCmd)
	}
	return sendCmd
}

// notifyNoProvider appends the standard "no provider connected" notice and
// returns a commit cmd. Used by SubmitToAgent and callers that need to do
// extra cleanup (e.g. skill ClearPending) before falling through to the
// same message.
func (m *model) notifyNoProvider() tea.Cmd {
	m.conv.AddNotice("No provider connected. Use /model to connect.")
	return tea.Batch(m.CommitMessages()...)
}

// HandleSkillInvocation runs the agent against a pending skill invocation.
// Skill button -> consume the queued invocation -> append to conv -> hand
// off to SubmitToAgent (carrying the plugin root so the resulting turn's
// hooks/tools see PLUGIN_ROOT pointing at that plugin). Provider check is
// up front because we want to clear the pending skill state when there's
// nothing we can do.
func (m *model) HandleSkillInvocation() tea.Cmd {
	if m.env.LLMProvider == nil {
		m.userInput.Skill.ClearPending()
		return m.notifyNoProvider()
	}
	displayMsg, fullMsg, pluginRoot := m.userInput.Skill.ConsumeInvocation()
	m.conv.Append(core.ChatMessage{Role: core.RoleUser, Content: fullMsg, DisplayContent: displayMsg})
	if pluginRoot != "" {
		m.services.Agent.SetPluginRoot(pluginRoot)
	}
	return m.SubmitToAgent(fullMsg, nil)
}
