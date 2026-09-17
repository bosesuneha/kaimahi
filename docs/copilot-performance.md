# Copilot Kubernetes tool benchmark

Measured 2026-09-16 against the installed Copilot CLI and `kind-kaimahi-p1`.
Agent: `hello-world-agent`. Prompt: `what pods are running?`. Enabled adapter
tool: `k8s-get-resources`. Nine sequential turns, three per model setting.
No native Orka Tasks were created by this benchmark.

The harness invoked the production tool-loop and HTTP executor, using the same
Copilot process flags, instruction lookup and tool lookup as chat. It decoded
CLI JSON events for phase timing without retaining raw event logs. Each turn
used a fresh temporary working directory. Model service/cache conditions were
not controlled; this is a small observational sample, not a latency guarantee.

## End-to-end results

| Model selection | Run 1 | Run 2 | Run 3 | Tool workflow |
| --- | ---: | ---: | ---: | --- |
| `auto` → `gpt-5.6-luna` | 14.678 s | 14.734 s | 14.715 s | 3/3 completed |
| `gpt-5.6-luna` | 18.960 s | 14.624 s | 14.497 s | 3/3 completed |
| `gpt-5.4-mini` | 7.725 s | 8.773 s | 12.748 s | Only run 3 completed the tool workflow |

Mini run 1 returned invalid adapter JSON. Run 2 produced a final answer without
calling the required tool and did not name expected running workloads. Neither
is a successful faster benchmark. Successful answers were checked for expected
workload names, not exhaustively scored for factual completeness.

## Auto-mode breakdown

Every successful auto turn made two CLI/model calls and one tool call. The first
adapter prompt was 1,430 bytes; the second was 3,249 bytes after appending the
tool request and 1,432-byte tool result. These are KMX prompt sizes, not the CLI's
full model-token count.

| Phase | Run 1 | Run 2 | Run 3 |
| --- | ---: | ---: | ---: |
| Agent instruction lookup | 0.283 s | 0.316 s | 0.272 s |
| Tool lookup/schema preparation | 0.589 s | 0.548 s | 0.519 s |
| First Copilot process | 6.538 s | 6.967 s | 5.994 s |
| Tool tunnel + execution | 0.457 s | 0.439 s | 0.439 s |
| Second Copilot process | 6.806 s | 6.456 s | 7.486 s |
| **Total** | **14.678 s** | **14.734 s** | **14.715 s** |

Within the two CLI processes:

| Combined phase | Run 1 | Run 2 | Run 3 |
| --- | ---: | ---: | ---: |
| Process start → model-call event | 6.730 s | 6.375 s | 6.882 s |
| Model dispatch (CLI-reported) | 4.953 s | 4.309 s | 4.887 s |
| Assistant-message event → process completion | 1.630 s | 2.710 s | 1.694 s |

CLI process time minus reported model dispatch is **8.39–9.11 seconds**, roughly
57–62% of end-to-end time. It includes runtime startup, auth/model preparation,
event handling and process shutdown; it is not all executable startup. Event
timestamps are wall-clock observations; outer durations use a monotonic clock.
Model dispatch itself includes provider/network time and cannot be identified as
pure GPU inference from these events.

Direct Luna selection removed some routing preparation but did not establish a
reliable improvement. One second model call took 8.98 s in dispatch, versus
2.70–2.92 s in the auto runs. Remote variability can dominate small local savings.

## Kubernetes tool cost

All complete tool runs used `{"phase":"Running","resource":"pods"}`.
Production tool execution, including tunnel startup, took 0.439–0.457 s.
A separate tunnel test measured:

- Tunnel bind announcement: 0.264 s (another sample: 0.307 s).
- First HTTP request through the tunnel: 51 ms.
- Subsequent HTTP requests: 24 ms each.

The tool is not the limiting factor. It already filters completed pods and emits
compact JSON. No persistent cluster configuration was changed for this benchmark.

## Optimization priorities

1. **Persistent Copilot SDK/server connection with native custom-tool callbacks.**
   Keep the CLI process alive across both model calls and subsequent turns. This
   targets most of the observed 8.4–9.1 s non-dispatch process cost and removes
   the prompt-encoded JSON protocol's reliability weakness. Not all of that cost
   is removable; benchmark the persistent implementation before promising a
   number. A 6–8 s warm-turn target is plausible from the measured budget, not a
   measured result.
2. **Consume the answer event before process shutdown.** The current wrapper
   waits for process exit before decoding output. Across two calls, that tail was
   1.6–2.7 s. A streaming reader can surface the final reply earlier; tool execution
   still needs a trusted completion boundary, and errors after an early event
   must not trigger duplicate actions. Simply killing the process on the first
   assistant event is not an equivalent implementation.
3. **Reuse Agent/tool snapshots within a turn.** Instructions and tool discovery
   separately read the Agent. Removing one duplicate read can save about 0.3 s.
   A session cache can target more of the 0.8–0.9 s lookup cost, with invalidation
   after `/tools`, `/agent`, and external configuration changes.
4. **Reuse the tool tunnel during chat.** About 0.25–0.4 s per tool call is startup,
   compared with tens of milliseconds for the request itself. Preserve endpoint
   ownership, context pinning and teardown/cancellation when adding reuse.
5. **Stream the final answer.** This improves time-to-first-visible-text more than
   total completion time. Do not stream partial adapter JSON into chat.

Changing the model or shaving bytes from this small tool result is not the first
optimization to pursue. The repeated process lifecycle is the largest consistent
cost, and smaller-model protocol failures cannot be counted as latency wins.
