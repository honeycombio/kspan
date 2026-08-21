package events

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client/fake" //nolint:staticcheck
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Initialize an EventWatcher, context and logger ready for testing
func newTestEventWatcher(initObjs ...runtime.Object) (context.Context, *EventWatcher, *fakeExporter, logr.Logger) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	log := zap.New(zap.UseDevMode(true))

	// Use a plain ObjectTracker rather than the fake client's default field-managed
	// tracker. The field-managed tracker recomputes managedFields on insert, which
	// discards the manager and operation recorded in the fixtures; the controller
	// synthesises its top-level span from exactly those values. A plain tracker
	// stores objects verbatim, matching the behaviour these tests were written
	// against. WithReturnManagedFields keeps managedFields on read responses, which
	// the fake client otherwise strips by default.
	tracker := clienttesting.NewObjectTracker(scheme, serializer.NewCodecFactory(scheme).UniversalDecoder())
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjectTracker(tracker).
		WithReturnManagedFields().
		WithRuntimeObjects(sanitizeForFakeClient(initObjs)...).
		Build()
	exporter := newFakeExporter()

	r := &EventWatcher{
		Client:   fakeClient,
		Log:      log,
		Exporter: exporter,
	}

	r.initialize(scheme)

	return ctx, r, exporter, log
}

// sanitizeForFakeClient adapts initial objects to the stricter controller-runtime
// fake client, which refuses to seed an object that has a deletionTimestamp but no
// finalizers. Some fixtures represent pods mid-deletion; we add a placeholder
// finalizer on a copy so the tracker accepts them. This does not affect the
// controller's trace output, which never inspects finalizers.
func sanitizeForFakeClient(objs []runtime.Object) []runtime.Object {
	out := make([]runtime.Object, 0, len(objs))
	for _, o := range objs {
		obj := o.DeepCopyObject()
		if accessor, err := meta.Accessor(obj); err == nil {
			if accessor.GetDeletionTimestamp() != nil && len(accessor.GetFinalizers()) == 0 {
				accessor.SetFinalizers([]string{"kspan.test/placeholder"})
			}
		}
		out = append(out, obj)
	}
	return out
}

func newFakeExporter() *fakeExporter {
	return &fakeExporter{}
}

// records spans sent to it, for testing purposes
type fakeExporter struct {
	SpanSnapshot tracetest.SpanStubs
}

func (f *fakeExporter) dump() []string {
	f.sort()
	spanMap := make(map[trace.SpanID]int)
	for i, d := range f.SpanSnapshot {
		spanMap[d.SpanContext.SpanID()] = i
	}
	var ret []string
	for i, d := range f.SpanSnapshot {
		parent, found := spanMap[d.Parent.SpanID()]
		var parentStr string
		if found {
			parentStr = fmt.Sprintf(" (%d)", parent)
		}

		message := attributeValue(d.Attributes, attribute.Key("message"))
		resourceName := attributeValue(d.Resource.Attributes(), semconv.ServiceNameKey)
		serviceName := attributeValue(d.Resource.Attributes(), attribute.Key("k8s.service"))
		ret = append(ret, fmt.Sprintf("%d: %s %s %s%s %s", i, resourceName, serviceName, d.Name, parentStr, message))
	}
	return ret
}

// ExportSpans implements trace.SpanExporter
func (f *fakeExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	f.SpanSnapshot = append(f.SpanSnapshot, tracetest.SpanStubsFromReadOnlySpans(spans)...)
	return nil
}

// Shutdown implements trace.SpanExporter
func (f *fakeExporter) Shutdown(ctx context.Context) error {
	return nil
}

func attributeValue(attributes []attribute.KeyValue, key attribute.Key) string {
	for _, lbl := range attributes {
		if lbl.Key == key {
			return lbl.Value.AsString()
		}
	}
	return ""
}

// Sort the captured spans so they are in a predictable order to check expected output.
// Use depth-first search, with edges ordered by start-time where those differ.
func (f *fakeExporter) sort() {
	sort.Stable(SortableSpans(f.SpanSnapshot))

	// Make a map from span-id to index in the set
	spanMap := make(map[trace.SpanID]int)
	for i, d := range f.SpanSnapshot {
		spanMap[d.SpanContext.SpanID()] = i
	}

	// Prepare vertexes for depth-first sort
	v := make([]*vertex, len(f.SpanSnapshot))
	for i := range f.SpanSnapshot {
		v[i] = &vertex{value: i}
	}
	topSpan := -1
	for i, s := range f.SpanSnapshot {
		if s.Parent.SpanID().IsValid() {
			p := spanMap[s.Parent.SpanID()]
			v[p].connect(v[i])
		} else {
			if topSpan != -1 {
				panic("More than one top span")
			}
			topSpan = i
		}
	}
	if topSpan == -1 { // no top span found; can't do DFS
		return
	}

	sortedSpans := make(tracetest.SpanStubs, 0, len(f.SpanSnapshot))
	t := dfs{
		visit: func(v *vertex) {
			sortedSpans = append(sortedSpans, f.SpanSnapshot[v.value])
		},
	}
	t.walk(v[topSpan])

	f.SpanSnapshot = sortedSpans
}

// SortableSpans attaches the methods of sort.Interface to tracetest.SpanStubs, sorting by start time.
type SortableSpans []tracetest.SpanStub

func (x SortableSpans) Len() int           { return len(x) }
func (x SortableSpans) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }
func (x SortableSpans) Less(i, j int) bool { return x[i].StartTime.Before(x[j].StartTime) }

// Single-use DFS implementation.
// Not using one from a library; see https://github.com/gonum/gonum/issues/1595
type vertex struct {
	visited    bool
	value      int
	neighbours []*vertex
}

func (v *vertex) connect(vertex *vertex) {
	v.neighbours = append(v.neighbours, vertex)
}

type dfs struct {
	visit func(*vertex)
}

func (d *dfs) walk(vertex *vertex) {
	if vertex.visited {
		return
	}
	vertex.visited = true
	d.visit(vertex)
	for _, v := range vertex.neighbours {
		d.walk(v)
	}
}
