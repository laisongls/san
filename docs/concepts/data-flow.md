# Data Flow: Input → Agent → Render

How a keystroke (or a cron fire, or a hub event) travels through the TUI
and becomes either a slash-command result or an agent response that the
user sees in their terminal.

## Cast

The TUI is a [Bubble Tea](https://github.com/charmbracelet/bubbletea) MVU
loop. Three Bubble Tea primitives drive everything:

- **`tea.Msg`** — an event entering the model (key press, window resize,
  spinner tick, custom in-process message).
- **`Update(msg)`** — pure function that mutates the model and returns a
  `tea.Cmd`.
- **`tea.Cmd`** — a function the framework will run; its return value is
  injected back as a new `tea.Msg`. This is how async work re-enters the
  model.

Input sources land as `tea.Msg`. **`SubmitToAgent`** is the single exit
to the running agent. Rendering happens via `tea.Println` (terminal
scrollback) plus `View()` (bottom UI strip).

```
   ┌──────────────────────────────────────────────────────────────┐
   │  Inputs                                                      │
   │                                                              │
   │   keyboard      slash command     cron       async hook      │
   │     │                │             │             │           │
   │     ▼                ▼             ▼             ▼           │
   │  handleSubmit  → SlashController  inject*    inject*         │
   │     │                │             │             │           │
   │     └────────────────┼─────────────┴─────────────┘           │
   │                      ▼                                       │
   │               SubmitToAgent(content, images)                 │
   │                      │                                       │
   │                      ▼ agent.Send (push to inbox)            │
   └──────────────────────┼───────────────────────────────────────┘
                          │
   ┌──────────────────────┼───────────────────────────────────────┐
   │  Agent loop          ▼                                       │
   │           ┌─────────────────────┐                            │
   │           │  Inbox → LLM → Tool │   ← runs in goroutine     │
   │           │     ↘    ↙          │                            │
   │           │     Outbox          │ → core.Event stream        │
   │           └─────────────────────┘                            │
   └──────────────────────┼───────────────────────────────────────┘
                          │
   ┌──────────────────────┼───────────────────────────────────────┐
   │  Render              ▼                                       │
   │             ContinueOutbox tea.Cmd                           │
   │                      │                                       │
   │                      ▼ tea.Msg                               │
   │                  Update → conv.Update → callbacks            │
   │                      │                                       │
   │                      ▼                                       │
   │             CommitMessages → tea.Println → scrollback        │
   │                      │                                       │
   │                      ▼                                       │
   │                   View() → bottom UI strip                   │
   └──────────────────────────────────────────────────────────────┘
```

## Path A — Text input

User types `hello`, presses **Enter**.

```
tea.KeyMsg('h')                  ── per keystroke
   │
   ▼
Update                            update.go
   ├─ case tea.KeyMsg → routeKeypress
   │     ├─ delegateToActiveModal  (no modal active)
   │     ├─ overlay overrides       (no overlay active)
   │     └─ handleTextareaShortcut
   │           └─ default for KeyRunes → (nil, false)
   │
   └─ routeToSubModel (no handler claims it)
   └─ updateTextarea                ← textarea consumes the rune
   ▼
View                              view.go      bottom UI shows "h▮"
```

Five rune-keystrokes later, textarea holds `hello`. User presses **Enter**:

```
tea.KeyMsg(Enter)
   │
   ▼
routeKeypress → handleTextareaShortcut
   └─ case tea.KeyEnter → m.handleSubmit()       update_submit.go
        │
        ▼
   handleSubmit
        Step 1: read textarea ────► "hello"
        Step 2: stream active? ───► no
        Step 3: → dispatchSubmission("hello")
                  │
                  ▼
   dispatchSubmission
        Step 1: "exit" literal? ──► no
        Step 2: prompt hook ──────► allowed
        Step 3: record to history
        Step 4: slash command? ───► no (no leading "/")
        Step 5: send to agent
                  ├─ plugin.ClearActivePluginRoot()
                  ├─ buildUserMessage("hello") → ChatMessage{Role: user}
                  ├─ conv.Append(msg)
                  ├─ userInput.Reset()
                  └─ SubmitToAgent(msg.Content, msg.Images)
                        │
                        ▼
   SubmitToAgent
        ├─ provider connected?    yes
        ├─ ensureAgentSession()    starts agent goroutine if needed
        ├─ sendToAgent ───────────► agent.Task inbox channel
        └─ returns ContinueOutbox cmd  (see Path D)
```

## Path B — Slash command

User types `/clear`, presses **Enter**. The path overlaps with Path A
up to Step 4:

```
handleSubmit → dispatchSubmission
   Step 1..3 same as Path A
   Step 4: runSlashCommandIfMatched("/clear")
              │
              ▼
   input.NewSlashCommandController(slashCommandEnv)         slash_command.go
              │
              ▼
   SlashCommandController.HandleSubmit
              │ ParseCommand("/clear") → ("clear", "")
              ▼
   handleClearCommand(c, ctx, "")
        ├─ env.StopAgentSession()         clears agent state
        ├─ env.PersistSession()           saves current conv
        ├─ env.Conversation.Clear()       wipes display
        ├─ env.Input.Reset()
        └─ returns (result="conversation cleared", cmd=nil, nil)
              │
              ▼
   c.deps.Conversation.AddNotice(result)    "conversation cleared"
   c.deps.CommitMessages()                  → tea.Println to scrollback
```

A slash command's handler reads live state via `env.*` (services),
mutates UI through callbacks (e.g. `env.PersistSession`), and returns
a short `result` string the controller wraps as a notice.

Some slash commands (`/loop`, `/init`) end up calling
`env.SubmitToAgent(prompt, nil)` to hand off to the agent — they
rejoin Path A at the SubmitToAgent step.

## Path C — Background trigger

Cron fires, an async hook produces a follow-up, or a subagent emits an
event on the hub. All three end at SubmitToAgent.

```
cron.Tick fires due jobs                trigger pkg pushes onto m.systemInput.CronQueue
hook produces continuation              trigger pushes onto m.systemInput.AsyncHookQueue
subagent completes                      eventHub publishes → m.mainEvents channel

────────── turn boundary ──────────

OnTurnEnd                          model_agent_events.go
   └─ drainTurnQueues                   model_turn_queue.go (priority order)
        ├─ user input queue?
        ├─ cron queue?       → injectCronPrompt(prompt)
        ├─ async hook queue? → injectAsyncHookContinuation(item)
        └─ main event hub?   → injectNotification(msg)

each inject*
   ├─ append notice / user-visible content to conv
   └─ SubmitToAgent(promptForAgent, nil)
```

All three converge on **SubmitToAgent**. Same provider check, same
`ensureAgentSession`, same `sendToAgent` push. There is no other way
to reach the agent's inbox from the TUI.

## Path D — Agent → render

The agent goroutine runs the inbox, calls the LLM, streams tokens,
emits tool calls, emits a final result. Every emission goes onto its
`Outbox` channel.

```
agent goroutine                         (runs in core.Agent.Run)
   │
   ▼
core.Event onto Outbox
   │
   ▼
ContinueOutbox tea.Cmd                  agent.go: reads one event
   │                                    re-arms itself to read the next
   ▼
tea.Msg (typed as a conv.* msg)
   │
   ▼
Update → routeToSubModel             update.go
   └─ conv.Update(m, &m.conv, msg)      app/conv/update.go
         │
         │ dispatches by event type, calls back into m via the
         │ conv.Runtime interface that *model implements:
         │
         ▼
   m.OnTurnBegin()                   turn start  ── model_agent_events.go
   m.OnTokenUsage(resp)                streaming token counts
   m.OnAutoCompact(info)           auto-compact ── model_compact.go
   m.OnToolResult(tr)              tool finished
   m.OnTurnEnd(result)             turn complete
        │
        ├─ m.CommitMessages()           model_scrollback.go
        │       │
        │       ▼ renderAndCommit → tea.Println(strings.Join(parts, "\n"))
        │       │
        │       ▼ terminal scrollback receives rendered Markdown
        │
        └─ m.drainTurnQueues()          model_turn_queue.go
                see Path C

   m.OnAgentStop(err)              turn ended (or canceled)
```

Two distinct render paths:

1. **`tea.Println`** — emits a line to the terminal **above** Bubble Tea's
   alt-screen. The terminal keeps the line in its native scrollback. Used
   for committed conversation messages (assistant replies, notices).
2. **`View()`** — called by Bubble Tea after every Update. Composes the
   bottom UI strip (textarea, status bar, modal overlays). This is the
   only thing rerendered on resize.

Streaming assistant tokens accumulate in `m.conv.Messages` but are not
`Println`-ed until they're "committed" — `renderAndCommit(checkReady=true)`
skips the last message if it's still streaming. The final
`OnTurnEnd` commits it.

## File pointers

| Path step | File |
|---|---|
| `Update` dispatch | [`internal/app/update.go`](../../internal/app/update.go) |
| Keyboard handling | [`internal/app/update_keys.go`](../../internal/app/update_keys.go) |
| Submit + SubmitToAgent | [`internal/app/update_submit.go`](../../internal/app/update_submit.go) |
| Slash command controller | [`internal/app/input/slash_command.go`](../../internal/app/input/slash_command.go) |
| Slash command env builder | [`internal/app/update_command.go`](../../internal/app/update_command.go) |
| Inject paths (cron/hook/hub) | [`internal/app/model_turn_queue.go`](../../internal/app/model_turn_queue.go) |
| Agent event callbacks | [`internal/app/model_agent_events.go`](../../internal/app/model_agent_events.go) |
| Scrollback commit | [`internal/app/model_scrollback.go`](../../internal/app/model_scrollback.go) |
| Conv event router | [`internal/app/conv/update.go`](../../internal/app/conv/update.go) |
| `agent.Send` / outbox poll | [`internal/app/agent.go`](../../internal/app/agent.go) |
| Bottom UI compose | [`internal/app/view.go`](../../internal/app/view.go) |
