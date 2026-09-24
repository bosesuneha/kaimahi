# `kmx agent lift`

> **Status: proposed. Nothing in this document is built.** Behaviour described
> as existing cites the file and line that establishes it. Behaviour proposed
> here says "would". No performance, cost or reliability claim is made.

`kmx agent lift` moves one agent from where it runs now to a destination that
can run it, without a terminal session. It is the non-interactive form of the
`/lift` slash command described in [`interactive-lift.md`](interactive-lift.md).

## Why this is not configuration or an upstream contribution

[`CONTRIBUTING.md`](../CONTRIBUTING.md) requires this section.

**Not configuration.** `/lift` is reachable only from inside a chat session and
refuses a non-terminal outright: `chat_orka_controls.go:81-83` returns
`/lift requires an interactive terminal`. There is no flag, env var or file
that turns the existing flow into a scriptable one.

**Not Orka.** Orka deploys what it is given. Selecting a destination, checking
that destination's prerequisites, resolving where inference comes from and
carrying one agent's definition between two clusters is an operation across two
platforms. Orka is one of them.

**Not `kubectl apply`.** The source Agent carries server-managed metadata,
status, a `providerRef` bound to the source namespace, and a `secretRef` whose
value must not travel. `portableLiftBundle` (`chat_lift.go:302-329`) exists
because a copy is not an apply.

**Not already built.** `README.md:36` states it: *"A standalone
`kmx agent lift` is not implemented yet."* `scripts/check-readme-front-door.py:36`
requires the command to appear on the front page, and it does not exist.

## What this proposes

One command that performs the same operation as `/lift`, with every decision
supplied as a flag rather than asked.

```
kmx agent lift <name> \
  --namespace <ns> \
  --to-context <ctx> | --subscription <id> --resource-group <rg> --cluster <name> \
  --to-namespace <ns> \
  --inference provider:<name> | keep-source \
  [--install-orka] [--install-k8s-tool] \
  [--plan]
```

## What this does not claim

Listed before the design, because they bound its value.

- **Not a replacement for `/lift`.** The interactive flow discovers: it
  searches subscriptions, fuzzy-matches AKS clusters, floats remembered
  choices, and offers verified alternatives when quota is exhausted. A flag
  cannot browse. This command is for a destination you can already name.
- **Not cluster provisioning.** It creates no AKS cluster. `kmx lift` is that
  command and stays separate ([naming](#open-questions)).
- **Not Azure Foundry provisioning.** See [scope](#scope-v1-refuses-foundry-creation).
- **Not a move.** The source is read and left running. Nothing is deleted.
- **Not a task replay.** No Task is created, before or after.
- **Not transactional.** A failure after the Provider is created leaves the
  Provider. `interactive-lift.md` records the same property for `/lift`:
  *"A partial deployment reports an error without deleting resources."*
- **Not a secret copy.** Secret values never travel. `portableLiftBundle`
  emits a value-free stub; the destination's Secret must already exist.

## Scope: v1 refuses Foundry creation

The existing flow has twenty decision points. Ten of them
(`chat_lift_foundry.go`, `chat_lift_quota.go`) provision Azure AI Services
accounts and model deployments, with quota preflight, three-region fan-out and
two separate billing confirmations (`chat_lift_foundry.go:209, 300`).

**This command would refuse `--inference foundry` and name `/lift` instead.**

The repository already settled the equivalent question once. `kmx lift`'s
`--payload` is required with no default, and `lift/plan.go:116-130` explains
why: *"This command bills money and installs a platform. A default would mean
an existing script quietly changed which one it deploys."* A flag that creates
a billable Azure account non-interactively is the same hazard with less review
in front of it.

Refusing is also honest about what is lost. The quota flow is not a
confirmation that could be replaced by `--yes`: it is a search across regions
for a combination that exists. A flag cannot conduct that search, and a command
that picked one silently would be choosing a region and a bill on the
operator's behalf.

Existing Foundry-backed Providers remain fully usable through
`--inference provider:<name>`. What v1 refuses is *creating* them.

## Design

### Flags and the decisions they replace

| Decision in `/lift` | Where asked | Flag |
|---|---|---|
| Target source, then context or subscription+cluster | `chat_lift.go:62,77,98,118` | `--to-context` XOR `--subscription`+`--resource-group`+`--cluster` |
| Orka CRDs missing — install? | `chat_lift_prerequisites.go:55` | `--install-orka`, else refuse |
| Orka controller unavailable — repair? | `chat_lift_prerequisites.go:75` | `--install-orka`, else refuse |
| Which inference | `chat_lift_prerequisites.go:136` | `--inference`, **required** |
| Kubernetes tool missing — install? | `chat_lift_prerequisites.go:170` | `--install-k8s-tool`, else refuse |
| Final deployment review | `chat_lift.go:221` | `--plan`, then re-run without it |
| Retry after failure | `chat_lift_deploy.go:60-63` | re-run the command |

`--inference` has **no default**, for the reason `--payload` has none. Keeping
the source configuration is frequently wrong: a source Provider pointing at a
cluster-local Ollama names a Service that does not exist at the destination.
The command would not guess which of those two an operator meant.

### The guard is not bypassed

`/lift` sets `worker.guarded = true` (`chat_lift.go:202`), which suppresses
`guardOrkaCreate` (`orka_create_online.go:60-88`). That is correct there: the
operator selected the destination from a list of live clusters, saw it in the
header, and confirmed a review pane naming it.

**A flag is not a picker.** A mistyped `--cluster`, a stale shell variable or a
copied command line are exactly the cases `internal/kmx/guard` exists for, and
its rule is that kmx never follows an ambient context (`guard.go:58-66`).

So this command would run the guard against the destination, with the same
behaviour every other writing command has: a local kind cluster proceeds with a
banner, anything else requires typed confirmation naming it or
`KAIMAHI_CONFIRM=<context>`.

### Reusing what is already headless

| Piece | Location | State |
|---|---|---|
| Deploy core | `orka_create_online.go:90-267` | Headless. Only seam is `App.operationProgress` (`app.go:42-43`), which may be nil |
| Reuse compare | `lift_reconcile.go:12-54` | Headless, gated by `App.liftReuse` |
| Metadata strip / rebind | `chat_lift.go:302-329` | Pure |
| Orka CRD check | `chat_lift_prerequisites.go:33-46` | Already an `*App` method, no TUI |
| Endpoint probe Job | `chat_lift_endpoint.go:105-153` | Headless, already tested with a fake kubectl |
| Install Orka | `orka.go:166` | Headless |
| Install k8s tool | `orka_k8s_tool.go:24` | Headless |
| Location records | `agent_locations.go:20-145` | Pure + IO, no TUI |

The work is therefore **not reimplementation**. It is a resolved-options struct,
a headless driver over these pieces, and refusals where `/lift` asks.

### Two definitions of ready

`OrkaReady()` (`orka.go:467`) checks the controller *and* the wrapper
Deployment, generation-aware. `/lift` instead inlines
`rollout status deploy/orka-controller-manager --timeout=10s`
(`chat_lift_prerequisites.go:71`).

These disagree. This command would use `OrkaReady()`, because a destination
missing the wrapper is a destination where an agent will not run, and reporting
it at deploy time rather than at first Task is the earlier failure.

Whether `/lift` should converge on the same check is a separate change and is
not proposed here.

### `--plan`

Prints the resolved destination, the source and destination Agent identity, the
inference selection, every prerequisite that is absent, and what would be
created versus reused — then stops. No cluster write occurs.

The precedent is `kmx lift --plan`: *"print what would be created, where, and
stop"* (`lift_commands.go`).

### Outcomes

Three are distinguishable and would be reported distinctly, because they need
different responses:

| Outcome | Meaning |
|---|---|
| Created | The destination had neither Provider nor Agent; both were created and became Ready |
| Reused | An identical spec was already present; it was not replaced |
| Conflict | A resource with that name exists with a different spec, or is terminating |

Reuse is decided by a server-side dry-run `replace` compare
(`lift_reconcile.go:12-54`) so that API defaults do not read as differences.
Nothing is ever replaced.

kmx returns one exit code today (`cmd/kmx/main.go` exits `1` for every
failure), so Conflict and an unreachable destination are currently
indistinguishable to a caller. The distinction lives in the message until a
classified exit status exists.

## The `az` seam

`liftDiscovery` (`chat_lift.go:22-37`) and `liftAzureWrite`
(`chat_lift_foundry.go:48-63`) call `exec.CommandContext` directly, bypassing
`App.Run`. They therefore carry no `Runner.Env`, no `Runner.Unset`, no echo,
and discard stderr to `io.Discard`. Tests can only substitute `az` by shadowing
`PATH` (`chat_lift_progress_test.go:37-42`).

Every kubectl call, by contrast, goes through `a.Command` → `a.Run`
(`orka_create_online.go:20-40`, `app.go:142-150`).

AKS target resolution needs `az`. This command would route those calls through
`App.Run` like every other subprocess. That is a small change to shared code and
it is why [phase 1](#implementation-plan) is separable and worth landing alone.

## Implementation plan

Four changes, each independently reviewable. Only the first touches shared code.

**1 — Route `az` through `App.Run`.** Replace the two raw `exec.CommandContext`
sites with the `orkaCapture` shape: prepare through the Runner, then give the
command its own deadline. The calls gain `Runner.Env` and `Runner.Unset`
handling, which a raw `exec.Command` cannot provide — it inherits the process
environment but cannot take a variable away. They are deliberately *not*
echoed: these run underneath a loading pane, and `Capture` already records the
rule that a read printing every query would be unreadable.

**2 — Extract a headless lift driver.** A `liftPlan` resolved-options struct and
a driver that executes the six steps against it, with the existing headless
pieces. `/lift` is not changed; the driver is new code the TUI could later call.
~600 lines including tests.

**3 — `kmx agent lift`.** The Cobra command, flag validation, `--plan`, the
guard call, outcome reporting. ~500 lines including tests.

**4 — Point `/lift` at the driver.** Optional, and David's call: the TUI
supplies the same resolved options from its pickers. Removes the duplicate
sequencing rather than adding a second one. Not proposed as part of this work.

Phases 1–3 leave `/lift` untouched.

## Testing

`chat_lift_endpoint_test.go:44-81` is the template: it drives
`verifyFoundryEndpoint` end to end against a fake `kubectl`, asserting the
argv, the create/get/delete sequence and that no Secret is deleted. No cluster,
no terminal.

The same approach covers: flag validation and mutual exclusion; a refusal when
Orka is absent without `--install-orka`; `--plan` writing nothing; created
versus reused versus conflict; the guard refusing an unconfirmed remote
context; and `--inference foundry` refusing with a message naming `/lift`.

Note that `liftAgent`, `chooseLiftTarget`, `prepareLiftOrka`,
`selectLiftInference` and `prepareLiftTools` have **no tests today**. Extraction
is therefore unguarded by existing coverage, and phase 2 must bring its own.

## Open questions

1. **`kmx lift` versus `kmx agent lift`.** One provisions an AKS cluster and
   installs a platform; the other moves one agent. Same verb, adjacent
   commands, different objects. `README.md:28` commits to `kmx agent lift`;
   [#194](https://github.com/kaimahi-agents/kaimahi/issues/194) specifies
   `render`/`deploy`/`status`/`evaluate` and does not mention lift. Whether this
   command is `deploy` with target selection folded in, or a distinct
   composite, is unresolved and is not settled by building it.
2. **Does v1 refuse Foundry, or accept fully-specified Foundry?** This document
   proposes refusing. An alternative is accepting an account and deployment
   that already exist while refusing to *create* either. That is a narrower
   refusal and may be the better line.
3. **Should `/lift` converge on `OrkaReady()`?** Not proposed here, but the two
   definitions should not both survive indefinitely.

## Rejected alternatives

**A `--yes` flag that answers every prompt.** The Foundry quota flow is a
search, not a confirmation. There is no correct answer for `--yes` to supply.

**Defaulting `--inference` to keep-source.** A source Provider naming a
cluster-local Ollama Service resolves to nothing at the destination, and the
failure arrives at first Task rather than at deploy.

**Bypassing the guard because `/lift` does.** `/lift` earns that through a live
picker and a review pane naming the destination. A command line has neither.

**Reimplementing the deploy path.** `createOrkaOnline` is already headless and
already carries schema validation, server admission, reuse comparison and
generation-aware readiness waits. A second implementation would be the
behaviour the prime directive exists to prevent.
