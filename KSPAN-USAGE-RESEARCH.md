# kspan - Usage Over the Years

Research into how kspan has been used, communicated, and adopted since 2020, against primary sources.
kspan turns Kubernetes Events into OpenTelemetry spans, joined by causality into traces.
Companion to [`KSPAN-FINDINGS.md`](./KSPAN-FINDINGS.md), which covers the code and the Weaveworks-vs-Honeycomb fork.

Compiled 2026-08-20 from two background research agents. Every claim links to its source.

## TL;DR

kspan is a well-known reference idea (800+ stars, cited inside the OpenTelemetry project) but has essentially zero live downstream ecosystem.
It was born as a Weaveworks experiment announced by Bryan Boreham in March 2021 and shown at KubeCon EU 2021, forked by Honeycomb in 2022, and has been de facto dormant since (Weaveworks development stopped Dec 2021; the Honeycomb fork went quiet after July 2024).
No production adoption is documented, no active fork exists, and its conceptual successor in the OTel Collector deliberately chose events-to-**logs** over kspan's events-to-**traces** model.

## Timeline

- **2020-10-23** - `weaveworks-experiments/kspan` repository created. https://github.com/weaveworks-experiments/kspan
- **2021-03-18** - Bryan Boreham's public launch tweet. https://x.com/bboreham/status/1372609182004883458
- **2021-03-22** - First independent third-party write-up (Felipe Cruz). https://www.felipecruz.es/visualizing-kubernetes-events-with-kspan/
- **2021-05-07** - KubeCon + CloudNativeCon Europe 2021 talk. https://kccnceu2021.sched.com/event/iE3j
- **2021-11-19** - OTel Operator issue #565 proposes k8s-events instrumentation, linking kspan. https://github.com/open-telemetry/opentelemetry-operator/issues/565
- **2021-12-14** - Last substantive commit to the Weaveworks repo; development effectively stops.
- **2022-01-27** - OTel Collector-Contrib issue #7408 "Convert Kubernetes events to traces" cites kspan. https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/7408
- **2022-09-26** - `honeycombio/kspan` fork created. https://github.com/honeycombio/kspan
- **2023-06-12** - Honeycomb blog documents internal use for crashloop detection. https://www.honeycomb.io/blog/how-honeycomb-monitors-kubernetes
- **2024-07-16** - Last commit to the Honeycomb fork; repo goes dormant.

## Origins and stated purpose

The original repo is `weaveworks-experiments/kspan` by Bryan Boreham at Weaveworks, created 2020-10-23.
https://github.com/weaveworks-experiments/kspan

The README states its purpose verbatim: "Most Kubernetes components produce Events when something interesting happens. This program turns those Events into OpenTelemetry Spans, joining them up by causality and grouping them together into Traces."
It has always been labelled "Work In Progress, under active evolution," and describes the same heuristics present in the current code: walking Owner References up the object chain (Pod -> ReplicaSet -> Deployment), delaying out-of-order events until a parent arrives, inferring child-of relationships from recently-seen owner events, and deriving a Trace ID by hashing an object's UID + generation.

There is no dedicated original Weaveworks blog post announcing kspan.
The public launch was Boreham's tweet plus the KubeCon talk; the only related Weaveworks blog he linked was the generic "Understanding Kubernetes Events" (https://www.weave.works/blog/understanding-kubernetes-events), and weave.works content is now largely defunct after Weaveworks shut down in 2024.
There is also no standalone design doc; the README is the de facto design document.

## Public communication

### Launch tweet (2021-03-18)

https://x.com/bboreham/status/1372609182004883458
Boreham announced kspan and pointed at the KubeCon talk, with a Gantt-chart screenshot of events from deployment-controller and kubelet joined by owner relationship over a timeline.
It was widely amplified (97 retweets, 481 likes).

### KubeCon EU 2021 talk (2021-05-07)

- Session page: https://kccnceu2021.sched.com/event/iE3j
- Title: "Traces from Events: A New Way to Visualise Kubernetes Activities"
- Speaker: Bryan Boreham (at Weaveworks when he built kspan; the sched page lists his later Grafana Labs affiliation)
- When: Friday, May 7, 2021, 13:45-14:20 CEST, Observability track, virtual
- Abstract: argues that Events already emitted across Kubernetes can be turned into OpenTelemetry Traces and visualised with Jaeger; covers modelling event data, converting Events into Spans, reconstructing parent-child relationships, and a live demo.
- Recording (CNCF/YouTube): https://www.youtube.com/watch?v=g5tHHD4crtQ (title and speaker confirmed; exact upload metadata was not retrievable from the JS-rendered page).

## The Honeycomb fork

`honeycombio/kspan`, created 2022-09-26, last commit 2024-07-16, 27 stars.
https://github.com/honeycombio/kspan

The README states it "was originally forked from https://github.com/weaveworks-experiments/kspan and has gone through several iterations by Honeycomb before landing here," and keeps the same core description and heuristics. It is still labelled "Work In Progress."

The clearest first-party statement of real usage is the Honeycomb blog post "How Honeycomb Monitors Kubernetes" by Nathan Lincoln, updated 2023-06-12.
https://www.honeycomb.io/blog/how-honeycomb-monitors-kubernetes
It describes kspan as a tool that "takes the events that your Kubernetes cluster emits and translates them to OpenTelemetry traces," and says Honeycomb used it internally to detect crashlooping pods: forwarding the converted events to Honeycomb, then filtering by `reason = Backoff` and grouping by object.

Notably, kspan does **not** appear in Honeycomb's official product docs, which instead document the OTel `k8s_events` receiver -> logs path (https://docs.honeycomb.io/send-data/kubernetes/opentelemetry/components).
There is no Honeycomb-authored blog post dedicated to kspan.

## What people have actually been able to do with it

Every documented use is an experiment or internal tool, consistently framed as not production-ready (data is held in memory and not persisted).

- **Visualise Kubernetes events in Jaeger.** Felipe Cruz, 2021-03-22 (four days after the launch tweet), deployed Jaeger as a backend, ran kspan as a pod, and viewed event traces in the Jaeger UI, contrasting it favourably with `kubectl describe`. https://www.felipecruz.es/visualizing-kubernetes-events-with-kspan/
- **Same Jaeger workflow, revisited in 2023.** cloudnative.quest, "Using kspan and Jaeger to visualize Kubernetes events as spans," 2023-08-06, showing continued community interest. https://www.cloudnative.quest/posts/observability/2023-08-06/using-kspan-and-jaeger-to-visualize-k8s-events-as-spans/
- **Tutorial content** on collecting Kubernetes events, e.g. isitobservable.io "How to collect Kubernetes events" and its companion repo `isItObservable/Episode-6---Kubernetes-Events`. https://isitobservable.io/observability/kubernetes/how-to-collect-kubernetes-events
- **Internal crashloop detection at Honeycomb** (see the Honeycomb blog above).

## Ecosystem and adoption today

### The two canonical repos (GitHub API, fetched 2026-08-20)

| Repo | Stars | Forks | Created | Last push | Status |
|---|---|---|---|---|---|
| weaveworks-experiments/kspan | 806 | 55 | 2020-10-23 | 2023-06-24 (bulk/no-op; real dev stopped ~Dec 2021) | Not archived, effectively abandoned |
| honeycombio/kspan | 27 | 5 | 2022-09-26 | 2024-07-16 | Not archived, dormant since mid-2024 |

Neither repo is archived or formally deprecated, but both are de facto dormant. A predecessor personal namespace, `github.com/bboreham/kspan`, also exists (visible on pkg.go.dev).

### Forks are all essentially dead

No fork of either repo has meaningful stars or ongoing development; enumerated forks sit at 0-1 stars.
Weaveworks forks include `classicvalues/kspan`, `lucas-giaco/kspan`, `mhausenblas/kspan` (1 star each), with many 0-star mirrors sharing identical bulk-push timestamps.
Honeycomb forks include `TylerHelmuth/kspan`, `dstrelau/kspan`, `totr/kspan` (most recent, 2024-07-18), all 0 stars; note `TylerHelmuth` is an OTel Collector maintainer, but the fork shows no meaningful activity.
`gh search repos kspan` surfaced no other real implementations (remaining hits are unrelated: Kerbal Space Program mods, k-spanning-tree algorithms, Android span libraries).

### The OpenTelemetry successor chose logs, not traces

This is the key ecosystem finding: kspan was influential but its events-to-traces model was never adopted upstream.

- OTel Collector-Contrib issue #7408, "Convert Kubernetes events to traces," opened by Juraj Kroliak (jpkrohling) on 2022-01-27, explicitly cites kspan as the inspiration and proposes extending the k8s events receiver to emit traces. It is now closed. https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/7408
- OTel Operator issue #565, "Add instrumentation for Kubernetes events," opened by Pavol Loffay on 2021-11-19, links kspan and proposes a `KubernetesEvents` field on the Instrumentation CRD to generate spans. https://github.com/open-telemetry/opentelemetry-operator/issues/565
- What actually shipped is the `k8seventsreceiver` (later deprecated in favour of the k8sobjects receiver), which converts Kubernetes events to OTel **logs**, not traces. kspan's specific span/causality approach has no maintained descendant in the Collector.

### Packaging

One Artifact Hub entry exists: `kspan` by publisher `puckpuck` (repo https://puckpuck.github.io/helm-charts), chart version 0.2.4, app_version 0.2, last indexed 2023-05-08. It is unofficial and unmaintained since 2023.
No official Weaveworks or Honeycomb Helm chart exists; both repos ship raw Kustomize/YAML manifests only.

## Gaps and negative findings

- No dedicated original Weaveworks blog post announcing kspan; the launch was a tweet plus the KubeCon talk.
- No standalone design doc separate from the README.
- No Honeycomb product-docs mention and no Honeycomb blog post dedicated to kspan; only the internal crashloop mention.
- No documented production adoption anywhere; every source frames kspan as experimental.
- No active fork of either repo, and no maintained successor project carrying the events-to-traces model forward.
- Exact YouTube upload metadata for the KubeCon recording could not be confirmed (JS-rendered page), though the talk is firmly dated 2021-05-07 via the sched page.
