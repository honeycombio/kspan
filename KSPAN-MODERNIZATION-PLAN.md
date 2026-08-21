# kspan - Modernization Plan

Goal: bring kspan back to a healthy, current, and end-to-end-verified state before any new feature work (e.g. Crossplane events-as-traces).
This round is **modernize + verify only**. Feature work is planned separately afterward.

## Decisions (confirmed)

- **Dependency targets:** latest stable everything (OTel v1.x latest, controller-runtime ~0.22 / k8s 0.36, Go 1.24).
- **Module path:** rename `github.com/weaveworks-experiments/kspan` -> `github.com/markandersontrocme/kspan`.
- **E2E backend:** Jaeger all-in-one accepting native OTLP gRPC on :4317, kspan pointed straight at it.
- **Scope:** deps current + unit tests green + working kind/Jaeger e2e. No Crossplane work yet.

## Verified starting state (2026-08-20)

- Builds and unit tests pass on Go 1.25 with the OLD pinned deps (`go build ./...`, `go test ./...`, `go vet` all clean).
- Tests are unit-only: fake controller-runtime client + fake span exporter + YAML "playback" harness asserting the trace tree as text. No envtest, integration, kind, or Jaeger harness exists yet.
- No CRDs (watches core `v1.Event` only); Makefile `generate`/`manifests`/`controller-gen`/`kustomize` targets are dead (`config/crd`, `hack/` absent).
- Toolchain drift: `go.mod` says go 1.13, Dockerfile builder golang 1.16.2, CI `setup-go` 1.20.

## The critical risk: OTel 0.19 -> 1.x

kspan does not use the normal Tracer/Span API. It hand-builds `tracesdk.SpanSnapshot` structs with deterministic hashed trace/span IDs and manually-set timestamps and pushes them straight to a `SpanExporter`. In OTel 1.0+:

- `go.opentelemetry.io/otel/sdk/export/trace` and `SpanSnapshot` are removed. `ExportSpans` now takes `[]sdktrace.ReadOnlySpan` (an interface).
- Migration path: build spans as `go.opentelemetry.io/otel/sdk/trace/tracetest.SpanStub`, call `.Snapshot()` to get a `ReadOnlySpan`, and export that. This preserves arbitrary IDs/timestamps - the behavior kspan depends on.
- OTLP exporter moves: `exporters/otlp` + `otlpgrpc.NewDriver` -> `exporters/otlp/otlptrace/otlptracegrpc`.
- `resource.NewWithAttributes` gains a schema-URL argument; `semconv` becomes versioned (e.g. `semconv/v1.26.0`).
- `codes.Ok` / `codes.Error` remain under `otel/codes`.

The existing unit tests are the regression net for this migration - keep them green at every step.

## Phases

### Phase 0 - Baseline & branch
- Create a modernization branch on `origin` (the fork).
- Commit the notes files (`KSPAN-FINDINGS.md`, `KSPAN-USAGE-RESEARCH.md`, this plan).
- Record the known-good green baseline (verified above).

### Phase 1 - Toolchain + module identity
- Rename module to `github.com/markandersontrocme/kspan`; update all internal imports (`main.go`, controllers).
- Bump `go` directive to 1.24; update Dockerfile builder and CI `setup-go` to match.
- Remove dead Makefile targets (CRD/codegen/kustomize); keep `build`, `test`, `run`, `docker-build`.
- Confirm still green.

### Phase 2 - Dependency modernization (green at each step)
Order matters; run `go test ./...` + `go vet` after each sub-step.
1. **k8s.io + controller-runtime together** (coupled). Fix: `Reconcile(ctx, req)` signature, fake-client builder API (`fake.NewClientBuilder().WithScheme(...).WithObjects(...).Build()`), `SetupSignalHandler()` returning ctx, `manager.RunnableFunc(ctx)`, logr v1 changes.
2. **OTel 0.19 -> 1.x** via `tracetest.SpanStub` (see risk section). Rewire the fake exporter in tests to the new `ReadOnlySpan` interface. Swap OTLP packages, resource schema URL, versioned semconv.
3. **grpc / prometheus / gomega** (mechanical bumps).
4. `go mod tidy`; ensure a clean `go.mod`/`go.sum`.

### Phase 3 - Local end-to-end (kind + Jaeger) [acceptance goal]
- Spin up a kind cluster.
- Deploy Jaeger all-in-one with native OTLP gRPC enabled on :4317.
- Build the kspan image (ko or docker), load into kind.
- Deploy RBAC + Deployment, `--otlp-addr` pointed at the Jaeger OTLP service.
- Generate events: `kubectl create deployment`, scale up/down.
- Confirm traces render in the Jaeger UI (Deployment -> ReplicaSet -> Pod lifecycle).
- Capture as a repeatable script / Make target + a README "Try it locally" section.

### Phase 4 - CI refresh
- Update `ko.yml` Go version to 1.24; refresh action versions (`checkout@v4`, `setup-go@v5`).
- Optional: add a kind-based smoke-test job.

## Acceptance criteria

- `go build ./...`, `go vet ./...`, `go test ./...` all green on current deps.
- `go.mod` on latest stable OTel/controller-runtime/k8s; module path renamed.
- A documented, repeatable kind+Jaeger run showing kspan-generated traces in the Jaeger UI.
- CI green on the modernization branch.

## Out of scope (next round)

- Crossplane events-as-traces investigation and any new features.
- OTel semantic-convention modeling changes beyond what the upgrade requires.
