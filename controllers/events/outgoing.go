package events

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	apitrace "go.opentelemetry.io/otel/trace"
)

type outgoing struct {
	sync.Mutex

	byRef    map[objectReference]*tracetest.SpanStub
	bySpanID map[apitrace.SpanID]*tracetest.SpanStub
}

func newOutgoing() *outgoing {
	return &outgoing{
		byRef:    make(map[objectReference]*tracetest.SpanStub),
		bySpanID: make(map[apitrace.SpanID]*tracetest.SpanStub),
	}
}

const timeFmt = "15:04:05.000"

// exportSpan snapshots a built span into the read-only form the SDK exporter
// requires and sends it, logging any error (there is nothing the caller can do).
func (r *EventWatcher) exportSpan(ctx context.Context, ref objectReference, span *tracetest.SpanStub) {
	if err := r.Exporter.ExportSpans(ctx, []sdktrace.ReadOnlySpan{span.Snapshot()}); err != nil {
		r.Log.Error(err, "failed to emit span", "ref", ref, "name", span.Name)
	}
}

// note we do not return errors, just log them here, because the one place it
// can happen refers to a previous span, so not something the caller can react to.
func (r *EventWatcher) emitSpan(ctx context.Context, ref objectReference, span *tracetest.SpanStub) {
	r.Log.Info("adding span", "ref", ref, "name", span.Name, "start", span.StartTime.Format(timeFmt), "end", span.EndTime.Format(timeFmt))
	r.outgoing.Lock()
	defer r.outgoing.Unlock()

	// Move the event source out of service.name (which we set to "kspan" so
	// all spans land in one place) and into a k8s.service attribute.
	var svcName string
	if v, ok := span.Resource.Set().Value(semconv.ServiceNameKey); ok {
		svcName = v.AsString()
	}

	merged, err := resource.Merge(span.Resource, resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String("kspan"), attribute.Key("k8s.service").String(svcName)))
	if err != nil {
		r.Log.Error(err, "failed to merge resource", "ref", ref, "name", span.Name)
	}
	span.Resource = merged

	if prev, found := r.outgoing.byRef[ref]; found {
		if !prev.StartTime.After(span.StartTime) {
			prev.EndTime = span.StartTime
		} else {
			r.Log.Info("New span before old span", "oldSpan", prev.Name, "oldTime", prev.StartTime.Format(timeFmt), "newSpan", span.Name, "newTime", span.StartTime.Format(timeFmt))
		}
		r.Log.Info("emitting span", "ref", ref, "name", prev.Name)
		r.exportSpan(ctx, ref, prev)
		// We do not remove from bySpanID at this time, in case it is needed for parent chains
	}
	r.outgoing.byRef[ref] = span
	r.outgoing.bySpanID[span.SpanContext.SpanID()] = span

	for parentID := span.Parent.SpanID(); parentID.IsValid(); {
		if parent, found := r.outgoing.bySpanID[parentID]; found {
			if span.EndTime.After(parent.EndTime) {
				//r.Log.Info("adjusting endtime", "parent", parent.Name, "from", parent.EndTime.Format(timeFmt), "to", span.EndTime.Format(timeFmt))
				parent.EndTime = span.EndTime
			}
			if parentID == parent.Parent.SpanID() {
				r.Log.Info("infinite loop!", "span", span.Name, "parent", parent.Name, "parentid", parentID)
				break
			}
			parentID = parent.Parent.SpanID()
		} else {
			break
		}
	}
}

func (r *EventWatcher) flushOutgoing(ctx context.Context, threshold time.Time) {
	r.outgoing.Lock()
	defer r.outgoing.Unlock()
	for k, span := range r.outgoing.byRef {
		if !span.EndTime.After(threshold) {
			r.Log.Info("deferred emit", "ref", k, "name", span.Name, "endTime", span.EndTime, "threshold", threshold)
			r.exportSpan(ctx, k, span)
			delete(r.outgoing.byRef, k)
			delete(r.outgoing.bySpanID, span.SpanContext.SpanID())
		}
	}
	// Now clear out anything old that is still in bySpanID
	for k, span := range r.outgoing.bySpanID {
		if !span.EndTime.After(threshold) {
			delete(r.outgoing.bySpanID, k)
		}
	}
}
