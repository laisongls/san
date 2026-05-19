# Technical Debt

This file tracks structural follow-ups that are not tied to a single feature.

## Current Candidates

- Continue reducing root `internal/app` file size by lifecycle responsibility.
- Document extension reload behavior across skills, commands, MCP, plugins, and
  settings.

### Code refactors flagged by `docs/packages/*/Known Violations`

- **God `Service` interfaces.** All resolved.
  ~~`mcp` (9 methods)~~ — `Tools` + `Servers` + `*mcp.Registry`.
  ~~`hook` (16 methods)~~ — `Handler` + `*hook.Engine`.
  ~~`session` (11 methods)~~ — `*session.Setup` direct.
  ~~`subagent` (9 methods)~~ — `*subagent.Registry` direct.
  ~~`skill` (11 methods)~~ — `*skill.Registry` direct.
  ~~`plugin` (21 methods)~~ — `*plugin.Registry` direct + existing
  package-level free functions.
  ~~`tool` (6 methods)~~ — `*tool.Registry` direct.
  ~~`cron` (10 methods)~~ — `*cron.Scheduler` direct (renamed from
  `*Store`; methods schedule, persistence is a detail).
  ~~`task` (8 methods)~~ — `*task.Tracker` direct (renamed from
  `*Manager`; tracks background bash/subagent tasks).
  ~~`command` (7 methods)~~ — `*command.Registry` direct.
  ~~`llm` (8 methods)~~ — `*llm.ClientFactory` direct (active
  provider/model handle + `*Client` factory). Wraps `*Setup`
  (Store + Provider + CurrentModel).
  ~~`agent` (11 methods)~~ — `*agent.Task` direct (foreground agent
  task lifecycle).
  ~~`setting` (14 methods)~~ — `*setting.Settings` direct (live,
  mutex-protected handle over `*setting.Data`; also the permission
  decision gate).
- **Escape-hatch methods on Service interfaces.** All resolved:
  ~~`MCP.Registry()`~~, ~~`Hook.Engine()`~~, ~~`Session.GetStore()`~~
  / ~~`Session.SetStore()`~~ — Service interfaces deleted in their
  respective packages; consumers depend on the concrete type or a
  narrow role interface.
- **Singleton via `Default()` / `DefaultIfInit()`.** Move construction
  into `cmd/gen` and pass the concrete service into
  `internal/app.newServices()` instead of pulling from each package's
  package-level singleton. Eliminates the two-flavor accessor pattern
  (`Default` panics; `DefaultIfInit` is nil-tolerant).
- ~~**`skill.AddPluginSkills` uses anonymous struct slice.**~~ Resolved
  by deleting the method entirely — it had zero callers and the
  `addPluginPath` / `additionalPaths` plumbing behind it was dead too.

### Adapter cleanups

- **Extract `core.Image` adapter out of `internal/image`.** `ToProviderData`
  and `ReadImageToProviderData` are the only reasons `image` cannot be
  `infrastructure`. Move them to a small adapter (e.g. consumer code in
  `internal/app/input` or a new `internal/image/adapter` subpackage that
  is itself `feature`) so `internal/image` can be reclassified back to
  `infrastructure`.

### Tests

- Unit test missing for `internal/agent.BuildParams` → `core.Config`
  translation (flagged in `docs/packages/agent.md`).
- `internal/agent/` has no package-local test file; coverage is
  end-to-end only.

### Documentation gaps (resolved 2026-05-18 in the docs/restructure branch)

- ~~`docs/guides/` only contains `explore-mode.md`.~~ Added
  `getting-started.md`, `writing-a-skill.md`, `writing-a-subagent.md`,
  `writing-a-plugin.md`.
- ~~`docs/design/decisions/` is empty.~~ Added
  `0001-layered-package-architecture.md`.
- ~~Infrastructure packages (`log`, `secret`, `filecache`, `markdown`)
  have no `docs/packages/*.md` page.~~ Added all four.

### Remaining documentation work

- `docs/packages/ui.md` Contract section is complete but the
  per-subpackage tour (`input/`, `conv/`, `hub/`, `trigger/`, `kit/`)
  is still squeezed into one page. Could split into
  `packages/ui/<sub>.md` once any single subpackage grows enough to
  warrant it.
