# Runtime adapters

KMX separates Agent platform (Orka/kagent), execution path (native/host), and
inference source (Foundry/Copilot/configured Provider).

`internal/kmx/runtime` defines `AgentRef`, `Adapter`, `Session`, capabilities,
commands, status and typed events without Kubernetes, vendor SDK or terminal
dependencies. Text events are observations; only a successful Send return means
the adapter's terminal-state checks passed. Discovery errors never trigger
fallback to another runtime with a same-named Agent.

`app/runtime_registry.go` registers Orka and kagent and preserves Orka-first auto
selection. Explicit runtime namespace rules and bare one-shot kagent behavior
remain compatible. New runtimes register discovery, construction and their
presentation driver at this composition root.

`runtime_session.go` bridges typed events to existing renderers. Shared timeline
and scanner commands come from the backend; Orka tool/lift commands are no longer
hardcoded in those drivers. Orka capabilities determine its available operations.
Configuration pickers remain application UI coordinators, outside Session.

`runtime_kagent.go` implements the same Session contract for readiness, connection,
streaming, session IDs and HITL continuation. Its existing terminal driver still
owns history/resume/governance commands and supplies an approval callback. This
preserves native input/error/cancellation semantics. It is not switched to the
Orka timeline until that UI can represent its history and approval state.

`runtime_inference.go` accepts resolved prompt/tools and an injected tool executor.
Copilot and Foundry model strategies no longer perform Orka discovery. Other
platforms can supply their own definition resolution and execution. Unknown
inference modes fail instead of silently selecting native execution.

The adapters currently live in `app` and bridge the existing renderer-oriented
protocol parsers. The shared contract is neutral; moving implementation files
into standalone packages can follow without changing it. Host inference still
uses the internal `copilotTool` DTO and renderer bridge. This is a chat/session
boundary, not a universal Agent CRUD or manifest-conversion API. Catalogue
digests/receipts remain catalogue-owned, and platform editors must preserve
fields they do not understand.

Tests exercise a third, non-Kubernetes runtime with typed events and its own
`/inspect` command through scanner and timeline dispatch, verifying no Orka
commands leak. Existing kagent session, streaming, HITL and governance tests
exercise its migrated connect/send path.
