//go:build e2e

package events

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	o "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
)

// spanSink is a minimal in-process OTLP/gRPC trace receiver that records every
// span pushed to it. It stands in for a real backend (Jaeger, a Collector)
// without their asynchronous ingestion, so the e2e assertion is deterministic.
// It is the same shape as the OpenTelemetry Collector's own telemetrygen e2e
// test, built from the OTLP proto + grpc packages already in the module graph
// rather than by importing the Collector.
type spanSink struct {
	coltracepb.UnimplementedTraceServiceServer

	mu    sync.Mutex
	spans []*tracepb.Span
	// resourceOf maps a span (by pointer identity) to the resource attributes of
	// the ResourceSpans it arrived in, so assertions can inspect the resource.
	resourceOf map[*tracepb.Span][]*commonpb.KeyValue
}

func newSpanSink() *spanSink {
	return &spanSink{resourceOf: make(map[*tracepb.Span][]*commonpb.KeyValue)}
}

func (s *spanSink) Export(_ context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rs := range req.GetResourceSpans() {
		attrs := rs.GetResource().GetAttributes()
		for _, ss := range rs.GetScopeSpans() {
			for _, span := range ss.GetSpans() {
				s.spans = append(s.spans, span)
				s.resourceOf[span] = attrs
			}
		}
	}
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

func (s *spanSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.spans)
}

// snapshot returns a copy of the captured spans so callers can inspect them
// without holding the lock.
func (s *spanSink) snapshot() []*tracepb.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*tracepb.Span, len(s.spans))
	copy(out, s.spans)
	return out
}

func (s *spanSink) resourceAttr(span *tracepb.Span, key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, kv := range s.resourceOf[span] {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue(), true
		}
	}
	return "", false
}

// startSpanSink starts the sink on a loopback address and returns it with the
// dial address and a stop func.
func startSpanSink(t *testing.T) (*spanSink, string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	sink := newSpanSink()
	srv := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(srv, sink)
	go func() { _ = srv.Serve(lis) }()
	return sink, lis.Addr().String(), srv.Stop
}

// TestOTLPExportE2E drives the controller with a known Event sequence and
// asserts on the spans it emits over a real OTLP/gRPC round-trip. This is the
// Tier-1 e2e gate from KSPAN-MODERNIZATION-PLAN.md: it exercises the real
// exporter and wire serialization that the fake-exporter unit tests skip, while
// staying deterministic via the mtime-driven playback harness.
func TestOTLPExportE2E(t *testing.T) {
	g := o.NewWithT(t)

	sink, addr, stop := startSpanSink(t)
	defer stop()

	// kspan's real OTLP/gRPC exporter, pointed straight at the in-process sink.
	ctx := context.Background()
	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(addr),
		otlptracegrpc.WithInsecure(),
	)
	g.Expect(err).NotTo(o.HaveOccurred())
	defer func() { _ = exp.Shutdown(context.Background()) }()

	const fixture = "testdata/deployment-2-pods.yaml"
	objs, maxTimestamp, err := getInitialObjects(fixture)
	g.Expect(err).NotTo(o.HaveOccurred())

	_, r, _ := newTestEventWatcherWithExporter(exp, objs...)
	defer r.stop()

	g.Expect(playback(ctx, r, fixture)).To(o.Succeed())

	threshold := maxTimestamp.Add(time.Second * 10)
	g.Expect(r.checkOlderPending(ctx, threshold)).To(o.Succeed())
	r.flushOutgoing(ctx, threshold)

	// Expected span-name multiset for testdata/deployment-2-pods.yaml, matching
	// the tree asserted by Test2PodDeploymentRollout (20 spans, one trace).
	wantNameCounts := map[string]int{
		"Deployment.Update":            1,
		"Deployment.ScalingReplicaSet": 4,
		"ReplicaSet.SuccessfulCreate":  2,
		"ReplicaSet.SuccessfulDelete":  2,
		"Pod.Scheduled":                2,
		"Pod.Pulling":                  1,
		"Pod.Pulled":                   2,
		"Pod.Created":                  2,
		"Pod.Started":                  2,
		"Pod.Killing":                  2,
	}
	const wantTotal = 20

	// The exporter Export call is synchronous, but poll to be robust against any
	// server-side scheduling; this is the require.Eventually pattern the OTel
	// e2e tests use.
	g.Eventually(sink.count, 10*time.Second, 20*time.Millisecond).Should(o.Equal(wantTotal),
		"all spans should arrive over OTLP/gRPC")

	spans := sink.snapshot()
	g.Expect(spans).To(o.HaveLen(wantTotal))

	// Index spans by their (hex) span id, and count names.
	byID := make(map[string]*tracepb.Span, len(spans))
	gotNameCounts := make(map[string]int)
	traceIDs := make(map[string]struct{})
	var roots []*tracepb.Span
	for _, sp := range spans {
		byID[string(sp.GetSpanId())] = sp
		gotNameCounts[sp.GetName()]++
		traceIDs[string(sp.GetTraceId())] = struct{}{}
		if len(sp.GetParentSpanId()) == 0 || allZero(sp.GetParentSpanId()) {
			roots = append(roots, sp)
		}
	}

	g.Expect(gotNameCounts).To(o.Equal(wantNameCounts), "span-name multiset should match the known trace tree")

	// Exactly one trace, exactly one root: the whole event sequence stitched
	// into a single causal tree.
	g.Expect(traceIDs).To(o.HaveLen(1), "all spans should belong to one trace")
	g.Expect(roots).To(o.HaveLen(1), "the trace should have a single root span")
	g.Expect(roots[0].GetName()).To(o.Equal("Deployment.Update"), "root should be the synthesised Deployment span")

	// Full parent/child connectivity: every non-root span's parent is present in
	// the exported set, so causality survived the wire with no dangling spans.
	for _, sp := range spans {
		if sp == roots[0] {
			continue
		}
		parent, found := byID[string(sp.GetParentSpanId())]
		g.Expect(found).To(o.BeTrue(), "span %q should have its parent in the exported set", sp.GetName())
		g.Expect(parent.GetTraceId()).To(o.Equal(sp.GetTraceId()), "child and parent should share a trace id")
	}

	// The Honeycomb single-dataset rewrite: every span's resource carries
	// service.name=kspan with the real component moved to k8s.service.
	for _, sp := range spans {
		svc, ok := sink.resourceAttr(sp, "service.name")
		g.Expect(ok).To(o.BeTrue(), "span %q should carry service.name", sp.GetName())
		g.Expect(svc).To(o.Equal("kspan"))
		k8sSvc, ok := sink.resourceAttr(sp, "k8s.service")
		g.Expect(ok).To(o.BeTrue(), "span %q should carry k8s.service", sp.GetName())
		g.Expect(k8sSvc).NotTo(o.BeEmpty(), "k8s.service should hold the originating component")
	}
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
