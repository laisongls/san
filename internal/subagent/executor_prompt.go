package subagent

import (
	"fmt"
	"strings"

	"github.com/genai-io/gen-code/internal/skill"
	"github.com/genai-io/gen-code/internal/task/tracker"
)

// buildSystemPrompt builds agent-specific Extra content for the system prompt.
// Identity, environment, instructions, and tool guidelines are already provided
// by system.System — this method only adds agent-specific content.
func (e *Executor) buildSystemPrompt(config *AgentConfig, permMode PermissionMode) string {
	var sb strings.Builder

	// Agent type header
	sb.WriteString("## Agent Type: ")
	sb.WriteString(config.Name)
	sb.WriteString("\n")
	sb.WriteString(config.Description)
	sb.WriteString("\n\n")

	// Mode-specific instructions
	switch permMode {
	case PermissionExplore:
		sb.WriteString("## Mode: Explore\n")
		sb.WriteString("You are in explore mode. You can use non-mutating research tools such as Read, Glob, Grep, WebFetch, and WebSearch. Do not modify files, execute shell commands, or change the workspace.\n\n")
	case PermissionEdit:
		sb.WriteString("## Mode: Edit\n")
		sb.WriteString("You are in edit mode. You can read and edit files using tools such as Read, Glob, Grep, Edit, and Write. Do not execute shell commands, spawn agents, or use tools that require separate approval.\n\n")
	}

	// Custom system prompt from config (lazily loaded from AGENT.md body)
	if sysPrompt := config.GetSystemPrompt(); sysPrompt != "" {
		sb.WriteString("## Additional Instructions\n")
		sb.WriteString(sysPrompt)
		sb.WriteString("\n\n")
	}

	// Preload skills into agent system prompt
	if len(config.Skills) > 0 && skill.DefaultIfInit() != nil {
		for _, skillName := range config.Skills {
			prompt := skill.Default().GetSkillInvocationPrompt(skillName)
			if prompt != "" {
				sb.WriteString("\n")
				sb.WriteString(prompt)
				sb.WriteString("\n")
			}
		}
	}

	// Guidelines
	sb.WriteString("## Guidelines\n")
	sb.WriteString("- Focus on completing your assigned task efficiently\n")
	sb.WriteString("- Return a clear summary when your task is complete\n")
	sb.WriteString("- If you encounter errors, report them clearly\n")

	return sb.String()
}

// toolProgressParams maps tool names to the parameter key used for display.
var toolProgressParams = map[string]string{
	"Read":       "file_path",
	"Write":      "file_path",
	"Edit":       "file_path",
	"Glob":       "pattern",
	"Grep":       "pattern",
	"Bash":       "command",
	"WebFetch":   "url",
	"WebSearch":  "query",
	"TaskCreate": "subject",
	"TaskUpdate": "taskId",
	"TaskGet":    "taskId",
	"TaskOutput": "task_id",
}

// formatToolProgress creates a progress message for a tool call in ToolName(args) format.
func formatToolProgress(toolName string, params map[string]any) string {
	if toolName == "Agent" {
		if label := formatAgentProgress(params); label != "" {
			return label
		}
		return toolName
	}

	// Task tools: show "TaskXxx(#id subject)" by looking up subject from store
	if label := formatTaskToolProgress(toolName, params); label != "" {
		return label
	}

	paramKey, ok := toolProgressParams[toolName]
	if !ok {
		return fmt.Sprintf("%s()", toolName)
	}

	value, ok := params[paramKey].(string)
	if !ok {
		return fmt.Sprintf("%s()", toolName)
	}

	if len(value) > 60 {
		value = value[:57] + "..."
	}

	return fmt.Sprintf("%s(%s)", toolName, value)
}

// formatTaskToolProgress formats task tool calls with "#id subject" display.
func formatTaskToolProgress(toolName string, params map[string]any) string {
	switch toolName {
	case "TaskCreate":
		subject, _ := params["subject"].(string)
		if subject == "" {
			return ""
		}
		if len(subject) > 50 {
			subject = subject[:47] + "..."
		}
		return fmt.Sprintf("TaskCreate(%s)", subject)

	case "TaskUpdate", "TaskGet":
		taskID, _ := params["taskId"].(string)
		if taskID == "" {
			return ""
		}
		subject := ""
		if t, ok := tracker.Default().Get(taskID); ok {
			subject = t.Subject
		}
		if subject != "" {
			if len(subject) > 40 {
				subject = subject[:37] + "..."
			}
			return fmt.Sprintf("%s(#%s %s)", toolName, taskID, subject)
		}
		return fmt.Sprintf("%s(#%s)", toolName, taskID)

	default:
		return ""
	}
}

func formatAgentProgress(params map[string]any) string {
	agentType, _ := params["subagent_type"].(string)
	mode, _ := params["mode"].(string)
	desc, _ := params["description"].(string)
	if desc == "" {
		desc, _ = params["prompt"].(string)
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
	}

	if agentType == "" {
		agentType = "general-purpose"
	}
	agentType = displayAgentName(agentType, PermissionMode(mode))
	if desc == "" {
		return fmt.Sprintf("Agent - %s", agentType)
	}
	return fmt.Sprintf("Agent - %s: %s", agentType, desc)
}

func displayNameFor(config *AgentConfig, req AgentRequest) string {
	if req.Name != "" {
		return req.Name
	}
	return displayAgentName(config.Name, requestPermissionMode(config, req))
}

func requestPermissionMode(config *AgentConfig, req AgentRequest) PermissionMode {
	if req.Mode != "" {
		return PermissionMode(req.Mode)
	}
	return config.PermissionMode
}

func displayAgentName(name string, mode PermissionMode) string {
	if isGenericAgentName(name) {
		switch mode {
		case PermissionExplore:
			return "Explorer"
		case PermissionEdit:
			return "Editor"
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "explore", "explorer":
			return "Explorer"
		case "edit", "editor":
			return "Editor"
		default:
			return "General"
		}
	}
	return shortAgentName(name)
}

func isGenericAgentName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "agent", "general", "general-purpose", "explore", "explorer", "edit", "editor":
		return true
	default:
		return false
	}
}

func shortAgentName(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	kept := make([]string, 0, 2)
	for _, word := range words {
		word = strings.ToLower(strings.TrimSpace(word))
		if word == "" || word == "current" || word == "change" || word == "changes" {
			continue
		}
		kept = append(kept, word)
		if len(kept) == 2 {
			break
		}
	}
	if len(kept) == 0 {
		return "Agent"
	}
	for i, word := range kept {
		kept[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(kept, " ")
}

func displayPermissionMode(mode PermissionMode) string {
	switch mode {
	case PermissionExplore:
		return "Explore"
	case PermissionEdit:
		return "Edit"
	default:
		return "Default"
	}
}
