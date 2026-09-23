# Chat latency profile: local, remote Foundry and Copilot

Measured 2026-09-18 with the production KMX call paths. Sources from the previous
2026-09-16 investigation: [Copilot](copilot-performance.md),
[Orka latency](orka-latency.md), [startup](orka-startup-performance.md), and
[Azure discovery](azure-discovery-performance.md).

## Main findings

1. Local CPU model processing dominates realistic tool turns: **68.193 s of
   73.534 s** in the fresh pod-list sample. Warm short answers expose orchestration
   overhead: **~6.17 s total for a 0.42–0.47 s model request**.
2. AKS + Foundry tool turns take **12.538–16.148 s**, of which model requests
   account for **6.195–9.097 s**. Warm worker startup is ~0.9 s; completion
   propagation is ~2.4–3.1 s. KMX's remote reads add further overhead.
3. Copilot CLI `-p` remains expensive: **11.497–13.280 s inside two processes**
   for successful turns, totaling **12.136–14.365 s**. One of three attempts
   failed the adapter JSON protocol. A failed shorter turn is not a speedup.
4. Observed Task-event → Job-event time is normally **43–69 ms** in fresh tool
   runs. There is no evidence here that increasing controller queue concurrency
   is the first optimization. Startup, model work, completion and client polling
   must be measured separately from queueing.

## Method and scope

`TestLiveChatPerformance` is an explicit environment-gated harness using
`runQuickstartOrkaTaskProfile`, `copilotToolLoop`, `copilotPromptModel` and the
production tool executor. Native runs share one bounded result session. Native
short-answer prompt: `Reply with exactly: ready. Do not call tools.` Tool prompt:
`what pods are running?`. No raw replies, credentials or prompts are logged by
the harness; timings, counts and basic outcome checks are emitted.

- Local: `kind-kaimahi-p1`, configured `qwen2.5:3b`, CPU Ollama. No model was
  resident immediately before the first short-answer run (`ollama ps` empty).
- Remote: saved AKS Agent location, Provider `openai`, Foundry deployment
  `gpt-5.1` in eastus2. Deployment model version and Azure capacity tier were not
  retrieved; the deployment name alone does not prove the underlying version.
- Copilot: CLI `1.0.86-0.`, `auto`, local Kubernetes read-only tool. The fresh
  harness did not record the concrete auto-selected model or CLI internal spans.
- Three native short-answer runs on each cluster; three remote tool turns; one
  local tool turn; three Copilot tool attempts. Small sequential samples per path,
  not percentile estimates or controlled same-model comparisons. Remote short
  turns and host Copilot runs overlapped; results can include shared-host noise.
- The local/remote native suites created **ten new Tasks**
  total (three local short, three remote short, three remote tools, one local
  tool). Their Jobs/results are retained under normal Orka lifecycle rules.
  Six remote Tasks invoked the existing billed Foundry deployment. Copilot
  attempts used the logged-in user's entitlement. No deployment configuration
  or cloud resources were provisioned for the benchmark.

The original exact-string check rejected all three local short replies even
though the Tasks succeeded and reported three output tokens. Their timing is
valid execution evidence but not strict instruction-following success. Remote
short replies passed the exact check. The retained harness tolerates trailing
punctuation for future short-response checks; the original local samples were
not rerun or retroactively marked valid. Tool-answer checks are limited to expected
workload-name substrings and successful workflow, not exhaustive factual grading.

## Fresh native client profile

Session setup is a **one-time separate cost**, excluded from each total below:
local short suite 1.184 s; local tool suite 1.312 s; remote short suite 3.137 s;
remote tool suite 2.876 s. The actual first chat turn pays this if not already open.

| Scenario/run | Task create | Execution/completion wait | Result + final checks | Timing read | Total |
|---|---:|---:|---:|---:|---:|
| Local short, first | 0.385 s | 70.313 s | 0.274 s | 0.006 s | **70.977 s** |
| Local short, warm 2 | 0.300 s | 5.583 s | 0.287 s | 0.004 s | **6.174 s** |
| Local short, warm 3 | 0.291 s | 5.552 s | 0.324 s | 0.004 s | **6.172 s** |
| Remote short, first | 0.806 s | 14.699 s | 0.877 s | 0.078 s | **16.461 s** |
| Remote short, warm 2 | 0.753 s | 7.674 s | 0.800 s | 0.079 s | **9.306 s** |
| Remote short, warm 3 | 0.711 s | 5.737 s | 0.782 s | 0.079 s | **7.309 s** |
| Local pod list | 0.321 s | 72.940 s | 0.268 s | 0.005 s | **73.534 s** |
| Remote pod list 1 | 0.661 s | 14.622 s | 0.792 s | 0.073 s | **16.148 s** |
| Remote pod list 2 | 0.686 s | 10.990 s | 0.789 s | 0.073 s | **12.538 s** |
| Remote pod list 3 | 0.780 s | 12.564 s | 0.781 s | 0.073 s | **14.198 s** |

## Fresh native server profile (nested inside client execution)

Do **not** add these values to the client table; they describe the same work.
Intervals derive from Orka event-store timestamps, not synchronized client clocks.

| Scenario/run | Task event → Job | Job → worker | Preparation | Model calls | Between calls | Result submit | Cleanup | Worker complete → Task succeeded |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Local short, first | .259 | 1.610 | .103 | 65.357 | — | .051 | .042 | 2.471 |
| Local short, warm 2 | .049 | .892 | .045 | .424 | — | .042 | .039 | 2.805 |
| Local short, warm 3 | .046 | .900 | .044 | .469 | — | .049 | .047 | 2.712 |
| Remote short, first | .219 | 5.827 | .105 | 2.656 | — | .056 | .058 | 5.179 |
| Remote short, warm 2 | .055 | .917 | .141 | 1.668 | — | .055 | .083 | 2.924 |
| Remote short, warm 3 | .063 | .897 | .139 | 1.585 | — | .054 | .051 | 2.805 |
| Local pod list | .043 | 1.035 | .050 | 68.193 | .138 | .058 | .083 | 3.160 |
| Remote pod list 1 | .055 | .913 | .100 | 9.097 | .239 | .073 | .075 | 2.361 |
| Remote pod list 2 | .064 | .924 | .146 | 6.195 | .263 | .067 | .057 | 3.053 |
| Remote pod list 3 | .069 | .972 | .115 | 6.597 | .275 | .062 | .052 | 3.055 |

All values in seconds. Short turns made one model call: local 1,230 input / 3
output tokens, remote 962 input / 11 output tokens. Tool turns made two calls:
local 2,980 input / 421 output tokens; remote 3,297–3,302 input / 759–865 output
tokens across calls. These token counts include more than the visible question.
Between-call duration includes tools/events, not pure tool-server runtime.

Warm local model time is only ~7% of total short-turn latency. Remote tool model
time is ~46–56% of end-to-end time. The first remote worker's 5.827 s startup
is an outlier; image pull, scheduler and pod-start subphases were not captured,
so it must not automatically be labeled image download or queue delay.

## Copilot profile and comparison

| Run | Lookup | CLI process 1 | Tool | CLI process 2 | Total | Outcome |
|---|---:|---:|---:|---:|---:|---|
| 1 | .566 s | 7.549 s | .518 s | 5.731 s | **14.365 s** | Tool workflow completed |
| 2 | .598 s | 6.191 s | .040 s | 5.307 s | **12.136 s** | Tool workflow completed |
| 3 | .569 s | 8.754 s | none | none | **9.323 s** | Invalid adapter JSON; failed |

First/second adapter inputs were 1,430 / 3,249 bytes. The second successful
turn reused the tool tunnel. Two completed workflows are not enough for p95
or a general reliability rate; the failure must still be included in the record.

The older three successful auto-mode samples averaged ~14.71 s. Their CLI event
breakdown measured **8.39–9.11 s outside reported model dispatch**, including
startup/auth/preparation and shutdown. Combined dispatch was 4.31–4.95 s.
The fresh measurements confirm the outer process cost remains dominant, but
do not independently remeasure that internal split.

**Is `copilot -p` slower than Foundry or direct Copilot auth?** Repeated `-p`
adds a demonstrated lifecycle cost. Authentication alone is not its explanation:
supplying a token to the same new-process-per-call design will not remove
initialization, prompt preparation, extra model calls and shutdown. Direct HTTP
Foundry or a persistent Copilot runtime can remove that particular lifecycle,
but the current Foundry route instead pays Orka/Kubernetes overhead. Observed
Foundry and Copilot tool-turn totals overlap, with different models/token counts.
There is no valid same-model provider-speed ranking from these samples.

No direct Copilot HTTP-auth benchmark was performed. Prefer the supported
[Copilot Go SDK](https://github.com/github/copilot-sdk/tree/main/go) persistent
runtime, native custom tools and streaming events to extracting login tokens or
depending on undocumented endpoint behavior. The SDK still uses a runtime
process by default; the gain is **reusing it**, not “SDK means no process”. Newer
SDK docs describe experimental in-process transport, but installed-runtime
compatibility and actual gains need measurement before adopting it.

## Queue time and responsiveness

The pinned v0.1.3 controller watches owned Jobs and also has a five-second running
fallback requeue. It is not a compulsory five-second sleep. Historical Pod/Job
timestamps showed most of the 2–3 s completion tail before Job completion became
visible. Fresh events show the same symptom locally and remotely, but cannot
separate kubelet publication, Job controller processing and Task reconciliation.

The current chat UI admits one active turn and allows drafting while it runs.
That is not a measured multi-request server queue. Saturation work needs a separate
bounded concurrent load test recording queue depth, workqueue wait, reconcile
duration, API throttling, scheduling, capacity waits and model-provider 429s.
Do not raise controller concurrency or shorten every requeue based on idle samples.

Both production routes buffer final output today: native KMX waits for terminal
Task/result verification; the Copilot wrapper runs with `--stream off` and parses
stdout after process exit. Time-to-first-visible-answer is therefore close to
completion time. The spinner helps perceived liveness but is not token streaming.

## Prioritized performance work

| Priority / owner | Change | Measured cost targeted |
|---|---|---|
| P0 KMX / Copilot | Persistent SDK runtime with native custom-tool callbacks | Historical 8.4–9.1 s repeated lifecycle budget; fresh 11.5–13.3 s total process time. Not all is removable. Also targets adapter JSON failures. |
| P0 local model | Foundry option or suitable accelerated local model; opt-in memory/tool prompt scope upstream | Local tool model time 68.2 s; historical uncached prompt evaluation 22.9 s of 23.7 s. Preserve correct tool use when evaluating smaller models. |
| P1 KMX | One persistent Kubernetes transport and Task watch instead of kubectl + 1 s sleeps | Warm remote create .66–.78 s, final read .78–.79 s; polling detection can cost a full request+sleep cycle. Avoid double-counting overlapping savings. |
| P1 Orka | Authoritative worker completion receipt consumed by Task controller | Typical 2.4–3.2 s completion tail. Require terminal outcome and result identity, not just early result upload. |
| P1 KMX/Orka | Stream answer/tool-status events; retain terminal success checks | Earlier first useful text; total compute may not improve. Label provisional streaming until outcome settles. |
| P2 Orka | Warm worker pool/session-capable chat execution | Warm ~.9–1.0 s worker startup; larger cold outliers. Evaluate newer runtime paths rather than bypassing Task accounting. |
| P2 model/output | Shorter verified tool results, selective schemas, concise answers and supported reasoning budgets | Foundry tool output 759–865 tokens; CPU tool output 421. Do not truncate away required facts. |
| P2 KMX setup | Broaden cached SDK-based Azure discovery | Historical AKS listing 11.2–13.2 s CLI vs 2.36–3.51 s SDK; this is setup latency, not chat-turn latency. |

Already implemented: cache-preserving Ollama weight preload; bounded native result
session reuse; one less result identity read; same-turn Agent fetch reuse in
Copilot; reusable tool tunnel. Do not count these as future savings again.

The separately developed revision inspector reads Agent and Tool definitions on connect.
That is outside these send-only samples and should be separately timed as
connect-to-editor latency. Avoid hashing/fetching every dependency on each render;
reuse snapshots and invalidate them on revision/dependency changes.

## What a complete next profile should add

This is an end-to-end **latency phase profile**, not CPU pprof or a controlled
provider trial. The following spans remain missing:

- Client submission → API admission → controller enqueue/dequeue, real queue
  length, reconcile CPU time, rate-limit/lock waits and worker scheduling stages.
- DNS, TCP/TLS, auth-token acquisition/refresh, provider time-to-first-token,
  provider queue time, decode throughput and retries. CLI-reported dispatch is
  network/provider time, not pure model inference.
- Copilot runtime boot vs session creation vs first/warm request, correlated
  streaming events, custom tool callback time and session-idle/exit tails.
- Definition hash, effective prompt/tool set, concrete model/version, reasoning
  settings and output limits for a controlled comparison. Run cold and warm
  separately and validate answer/tool correctness before aggregating latency.
- At least enough repeated samples for defensible median/tail comparisons, plus
  bounded concurrency 1/2/4 to distinguish queuing from fixed lifecycle cost.

## Reproduce

The harness skips in ordinary CI. Each native run creates real Tasks and consumes
the configured model; Copilot runs consume the logged-in account's model access.
Use an explicitly chosen kubeconfig/context. `KMX_PROFILE_RUNS` accepts 1–10.

```sh
KMX_PROFILE_MODE=native KMX_PROFILE_CONTEXT=kind-kaimahi-p1 \
  go test ./internal/kmx/app -run '^TestLiveChatPerformance$' -v -count=1 -timeout=540s

KMX_PROFILE_MODE=native KMX_PROFILE_SCENARIO=tools KMX_PROFILE_RUNS=1 \
  KMX_PROFILE_CONTEXT=kind-kaimahi-p1 \
  go test ./internal/kmx/app -run '^TestLiveChatPerformance$' -v -count=1 -timeout=540s

KMX_PROFILE_MODE=copilot KMX_PROFILE_CONTEXT=kind-kaimahi-p1 \
  go test ./internal/kmx/app -run '^TestLiveChatPerformance$' -v -count=1 -timeout=540s
```

For the remote run, use the saved Agent's private kubeconfig via `KUBECONFIG` and
its context via `KMX_PROFILE_CONTEXT`. No subscription identifiers or credential
material need to be copied into this document. Foundry benchmark authentication
used the existing deployed Provider, not a new Entra integration.
