---
package: github.com/genai-io/gen-code/internal/agent
layer: feature
---

# agent

Owns the **main agent session lifecycle** — construction, start, stop, and
TUI-facing send/permission/outbox plumbing for the single foreground agent.

## Purpose

`internal/app` runs exactly one foreground agent session at a time. This
package is the seam between that TUI shell and the underlying agent loop in
[`packages/core.md`](core.md). The shell starts a session, hands user input
to it, observes its outbox, and routes permission requests back to the user.

Subagents (parallel background agents) are owned separately by
[`packages/subagent.md`](subagent.md); cron and async triggers feed into the
same `Send` path used by user input.

## Contract

Foreground agent session lifecycle. Holds the single core.Agent + permission bridge for the TUI session. The package exposes `*Session` directly — no Service interface.

```go
package agent

// Session is the opaque handle. Type exported; fields unexported.
type Session struct { /* internal fields */ }

func (s *Session) Start(params BuildParams, messages []core.Message) error
func (s *Session) Stop()
func (s *Session) Active() bool
func (s *Session) Send(content string, images []core.Image)
func (s *Session) Outbox() <-chan core.Event
func (s *Session) PermissionBridge() *PermissionBridge
func (s *Session) PendingPermission() *PermBridgeRequest
func (s *Session) SetPendingPermission(req *PermBridgeRequest)
func (s *Session) System() core.System

// Package-level access
func Initialize(opts Options)
func Default() *Session
func SetDefaultSession(s *Session)  // test-only
func ResetDefaultSession()          // test-only
```


## Internals

- `service` (`session.go`) is the only implementation. It tracks one
  `*core.Agent` plus its cancellation context, a `PermissionBridge`, and a
  pending `PermBridgeRequest` (TUI approval handshake).
- `build.go` translates `BuildParams` (model, identity, skills, tools,
  permission mode, cwd, ...) into a `core.Config` for `core.NewAgent`.
- `permission.go` owns the bridge: a thread-safe channel pair that turns
  asynchronous permission asks into synchronous TUI approval modals.
- No persistence here — session/transcript state lives in
  [`packages/session.md`](session.md).

## Lifecycle

- Construction: `Initialize(Options{})` runs at app startup, registering the
  singleton.
- Per-session: `Start(params, messages)` builds a `core.Agent` and launches
  its `Run` goroutine. The agent's outbox is the only return channel.
- Termination: `Stop()` cancels the run context. Outbox closes via
  `core.Agent` shutdown. `Active()` flips to false.
- Reentrancy: methods are guarded by a mutex; concurrent `Send` from the
  user-input goroutine and the cron/trigger goroutines is the design
  intent.

## Tests

```
internal/agent/                — no package-level test file today.
                                  Coverage is exercised end-to-end via
                                  internal/app and integration tests.
```

A unit test for `BuildParams → core.Config` translation is missing and
worth adding (logged in `notes/tech-debt.md`).

## See Also

- Code: `internal/agent/`
- Underlying primitive: [`packages/core.md`](core.md) (the `Agent`
  interface and the inbox/outbox event model)
- Background agents: [`packages/subagent.md`](subagent.md)
- Permission model: [`concepts/permission-model.md`](../concepts/permission-model.md)
- Layer: `feature` (see [`reference/dependency-rules.md`](../reference/dependency-rules.md))
