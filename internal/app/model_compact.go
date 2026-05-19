// Conversation compaction: assembling a compact request from current
// messages, handling the agent's auto-compact event, and applying a manual
// /compact result. Both paths flush remaining scrollback, clear the live
// conversation, and reseed it with the compact summary so the next user
// turn restarts from a fresh, shorter history.
package app

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/genai-io/gen-code/internal/app/conv"
	"github.com/genai-io/gen-code/internal/app/kit"
	"github.com/genai-io/gen-code/internal/core"
	"github.com/genai-io/gen-code/internal/filecache"
	"github.com/genai-io/gen-code/internal/hook"
)

func (m *model) BuildCompactRequest(focus, trigger string) conv.CompactRequest {
	var hookEngine *hook.Engine
	if m.services.Hook != nil {
		hookEngine = m.services.Hook
	}
	return conv.CompactRequest{
		Ctx:        context.Background(),
		Client:     m.buildLLMClient(),
		Messages:   m.conv.ConvertToProvider(),
		Focus:      focus,
		HookEngine: hookEngine,
		Trigger:    trigger,
	}
}

func (m *model) OnAutoCompact(info core.CompactInfo) tea.Cmd {
	scrollbackCmds := m.commitAllMessages()
	boundaryStyle := lipgloss.NewStyle().Foreground(kit.CurrentTheme.Muted)
	boundary := boundaryStyle.Render(fmt.Sprintf("✻ Conversation compacted — %d messages summarized (scroll up for history)", info.OriginalCount))

	m.conv.Clear()
	m.env.ResetContextDisplay()
	token := m.userInput.Provider.SetStatusMessage("compacted")
	m.conv.Append(core.ChatMessage{Role: core.RoleUser, Content: core.FormatCompactSummary(info.Summary)})

	if m.services.Hook != nil {
		m.services.Hook.ExecuteAsync(hook.PostCompact, hook.HookInput{Trigger: "auto"})
	}
	// Auto-compact summarizes the conversation history, dropping any
	// system-reminder content that previously rode on user messages. Re-enqueue
	// session-level reminders so they reattach to the next user turn and the
	// LLM still has skills/memory/etc. context.
	if m.services.Reminder != nil {
		m.services.Reminder.EnqueueAllProviders()
	}

	scrollPart := tea.Sequence(append(scrollbackCmds, tea.Println(boundary), tea.ClearScreen)...)
	return tea.Batch(scrollPart, m.ContinueOutbox(), kit.StatusTimer(3*time.Second, token))
}

// OnCompactResult handles manual /compact results.
// Stops the agent so the next user message restarts it with compacted messages.
func (m *model) OnCompactResult(msg conv.CompactResultMsg) tea.Cmd {
	if msg.Error != nil {
		m.conv.Compact.Complete(fmt.Sprintf("Compaction could not be completed: %v", msg.Error), true)
		return tea.Batch(m.CommitMessages()...)
	}
	m.conv.Compact.Complete(fmt.Sprintf("Condensed %d earlier messages.", msg.OriginalCount), false)
	scrollbackCmds := m.commitAllMessages()
	boundaryStyle := lipgloss.NewStyle().Foreground(kit.CurrentTheme.Muted)
	boundary := boundaryStyle.Render(fmt.Sprintf("✻ Conversation compacted — %d messages summarized (scroll up for history)", msg.OriginalCount))

	m.conv.Clear()
	m.env.ResetTokens()
	token := m.userInput.Provider.SetStatusMessage("compacted")
	m.StopAgentSession()

	m.conv.Append(core.ChatMessage{Role: core.RoleUser, Content: core.FormatCompactSummary(msg.Summary)})

	var restoredFiles []filecache.RestoredFile
	if m.env.FileCache != nil {
		restoredFiles, _ = m.env.FileCache.RestoreRecent()
		if len(restoredFiles) > 0 {
			restoredContext := filecache.FormatRestoredFiles(restoredFiles)
			m.conv.Append(core.ChatMessage{Role: core.RoleUser, Content: restoredContext})
			m.conv.AddNotice(fmt.Sprintf("Restored %d recently accessed file(s) for context.", len(restoredFiles)))
		}
	}
	if m.services.Hook != nil {
		m.services.Hook.ExecuteAsync(hook.PostCompact, hook.HookInput{Trigger: msg.Trigger})
	}
	// Manual /compact also drops conversation-history reminders. Re-enqueue
	// providers so the next user turn carries fresh session-level context.
	if m.services.Reminder != nil {
		m.services.Reminder.EnqueueAllProviders()
	}

	scrollPart := tea.Sequence(append(scrollbackCmds, tea.Println(boundary), tea.ClearScreen)...)
	return tea.Batch(scrollPart, tea.Batch(m.CommitMessages()...), kit.StatusTimer(3*time.Second, token))
}

func (m *model) OnTokenLimitResult(msg kit.TokenLimitResultMsg) tea.Cmd {
	m.userInput.Provider.FetchingLimits = false
	var content string
	if msg.Error != nil {
		content = "Error: " + msg.Error.Error()
	} else {
		content = msg.Result
	}
	m.conv.AddNotice(content)
	return tea.Batch(m.CommitMessages()...)
}
