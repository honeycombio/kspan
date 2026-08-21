# kspan - Findings

Notes from getting familiar with the `honeycombio/kspan` codebase, including its Weaveworks origins.

## What kspan is

kspan is a single Kubernetes controller that watches core `v1.Event` objects and turns them into OpenTelemetry spans, stitched into traces by causality.
Most Kubernetes components emit an Event when something interesting happens; kspan reconstructs the causal relationships between those events and exports them as OTLP traces so tools like Honeycomb can visualise cluster activity as a trace tree.

Lineage: `weaveworks-experiments/kspan` -> several Honeycomb iterations -> this repo.
It is explicitly a work in progress.
The Go module path is still `github.com/weaveworks-experiments/kspan`.

### Stack

- Go 1.13 (per `go.mod`).
- `sigs.k8s.io/controller-runtime` v0.6.
- OpenTelemetry Go SDK v0.19 (old API: `SpanSnapshot`, `sdk/export/trace`).
- Exports via OTLP/gRPC.
- Deployed as a single-replica Deployment with a ClusterRole binding (`config/`).

## How it works

The core problem: a Kubernetes Event only says "X happened to object Y".
kspan reconstructs causality (which event caused which) to build trace trees, using three moving pieces plus heuristics.

### `main.go`

Sets up the OTLP exporter (address, headers, TLS via flags; `--capture-to` for recording), builds a controller-runtime manager, and registers one `EventWatcher` reconciler `For(&corev1.Event{})`.

### `event_controller.go` - the reconcile loop

For each event, `emitSpanFromEvent` tries in order to find a parent trace context:

1. A recent event matching the exact actor->object ref.
2. A recent event for the owner alone.
3. Direct mapping via the `topLevelSpan: "true"` annotation, which roots a new trace at the object.
4. Walking owner references looking for recent activity.
5. Trying a distinct actor object.

If nothing matches, the event is held pending (out-of-order arrival is expected) and retried when new spans appear.

### `object.go` / `event.go` - identity and heuristics

- Deterministic IDs by hashing (fnv): TraceID = `UID + generation` (so a spec change starts a new trace), SpanID from UID+generation, event SpanID from event UID+count.
- Message-parsing heuristics to re-parent events onto the object they actually describe: deployment-controller "Scaled ... replica set X", replicaset-controller "Created pod: X", statefulset-controller "create Pod X". These reassign the span from the owner to the sub-object.
- Top-level objects with no owner and no recent event become new trace roots (`createTraceFromTopLevelObject`), deriving the "source" service from managedFields (`f:spec`).

### `recent.go` - the causality cache

Short-lived cache keyed by `actionReference` -> span contexts.
5s "recent window" (the heuristic that recent owner activity caused this event), 5min expiry.
This is the core causality-joining state.

### `pending.go` - the out-of-order queue

`checkPending` retries repeatedly as long as new spans keep getting emitted.
`checkOlderPending` gives up after a threshold and force-creates a trace from the topmost owner, or drops the event.

### `outgoing.go` - the span buffer

Buffers spans by ref/spanID so `EndTime` can be back-filled (a span's end = the next span's start; parents stretch to cover children), then flushes after `2 * recentWindow`.
This is why spans are not exported immediately.

### `playback.go` + `pkg/mtime` - test harness

Capture/replay harness for deterministic testing.
`--capture-to` dumps events and initial objects to YAML; playback re-feeds them while `mtime` forces the clock.
Tests drive this with `controllers/events/testdata/deployment-2-pods.yaml`.

## Weaveworks origins vs the Honeycomb fork

Full history is preserved in this repo (128 commits), and the split is clean.

### Weaveworks era (the whole engine)

Commits from `f18cc7f Initial commit` through PR #35 (`12eefbe`, the Docker-build fix).
Almost entirely **Bryan Boreham** (78 commits across his two emails).
Essentially all the actual functionality was written here:

- The `EventWatcher` controller and the whole event->span pipeline (`emitSpanFromEvent`, the ordered parent-lookup cascade).
- Causality reconstruction: the `recent` store and window heuristic, the `pending`/out-of-order queue, owner-reference walking.
- Deterministic hashed IDs, top-level-object trace creation, the `topLevelSpan` annotation.
- The message-parsing heuristics for Deployment/ReplicaSet/StatefulSet.
- The `outgoing` span buffer with end-time back-filling.
- The capture/playback and `mtime` testing harness, and the controller-runtime 0.6 upgrade.

### Honeycomb era (adaptation, not algorithm work)

Begins around `c011070` / `4e550b9` (the CI switch) and runs to `HEAD`.
Since the fork it is roughly 1800 changed lines, but almost all of it is CI, docs, and dependencies.
Only four commits touch `.go` logic, and two of those are the same person doing OTel plumbing:

| Commit | Author | What it did |
|---|---|---|
| `7f9a660` | Pierre Tessier (puckpuck) | Move to OTel Go SDK 0.19.0 |
| `34aab59` | Pierre Tessier | Add `--otlp-secured` and `--otlp-headers` flags (Honeycomb endpoint: API-key header + TLS) |
| `6b57e96` | Martin Holman | The one real behavioral change (below) |
| `dbe0e21` | Nathan Lincoln | Bugfix: don't segfault when there's no changed time in `getUpdateSource` |

**The one meaningful behavioral change (`6b57e96`)** is Honeycomb-specific product adaptation, not algorithm work.
kspan originally set OTel `service.name` to the Kubernetes event source (kubelet, deployment-controller, etc.), which is high-cardinality.
Honeycomb creates one dataset per `service.name`, so Holman's patch (in `outgoing.go`) rewrites every span at emit time to a static `service.name = "kspan"` and moves the original component name into a `k8s.service` attribute, so everything lands in a single Honeycomb dataset.

Everything else Honeycomb contributed is repo hygiene:

- CircleCI -> `ko`/GitHub Actions builds, multiarch images.
- README rewrite (documenting the fork lineage and the new `example.png`).
- CODEOWNERS / SECURITY / SUPPORT / issue templates.
- Dependabot bumps (grpc, client-go 0.27, gomega, prometheus).

**Bottom line:** Weaveworks wrote the entire event-to-span causality engine as an experiment; Honeycomb adopted it largely as-is and adapted it to ship into Honeycomb's platform (OTLP auth/TLS, a single-dataset service-name scheme, modern CI, dependency upkeep) without meaningfully altering the core algorithm.

## Things worth flagging

- Stale dependencies: OTel v0.19 is very old, and its API (`SpanSnapshot`, `sdk/export/trace`) is long gone upstream. `go.mod` still declares Go 1.13. Any real work here likely starts with a large OTel and controller-runtime upgrade.
- Heuristics are string-matching English event messages, which is brittle across Kubernetes versions.
- There is a commented-out staleness check in `recent.go` (`lookupSpanContext`): the recent window is not actually enforced on lookup, only on expiry.
