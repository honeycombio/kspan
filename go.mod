module github.com/weaveworks-experiments/kspan

go 1.13

require (
	github.com/go-logr/logr v1.2.3
	github.com/imdario/mergo v0.3.9 // indirect
	github.com/onsi/gomega v1.27.4
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.15.1
	go.opentelemetry.io/otel v0.19.0
	go.opentelemetry.io/otel/exporters/otlp v0.19.0
	go.opentelemetry.io/otel/sdk v0.19.0
	go.opentelemetry.io/otel/trace v0.19.0
	google.golang.org/grpc v1.55.0
	k8s.io/api v0.27.2
	k8s.io/apimachinery v0.27.2
	k8s.io/client-go v0.27.2
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/yaml v1.3.0
)
