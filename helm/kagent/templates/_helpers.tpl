{{/*
Create a default fully qualified app name.
*/}}
{{- define "kagent.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- if not .Values.nameOverride }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kagent.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "kagent.selectorLabels" . }}
{{- if .Chart.Version }}
app.kubernetes.io/version: {{ .Chart.Version | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: kagent
{{- with .Values.labels }}
{{ toYaml . | nindent 0 }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "kagent.selectorLabels" -}}
app.kubernetes.io/name: {{ default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*Default model name*/}}
{{- define "kagent.defaultModelConfigName" -}}
default-model-config
{{- end }}

{{/*
Expand the namespace of the release.
Allows overriding it for multi-namespace deployments in combined charts.
*/}}
{{- define "kagent.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Watch namespaces - transforms the list of namespaces cached by the controller into a comma-separated string.
controller.watchNamespaces is an explicit override; otherwise the watch scope is the resolved RBAC scope
(kagent.rbacNamespaces), so the controller never watches a namespace its Roles do not cover and never
holds a cluster-wide cache when RBAC is namespaced. An explicit rbac.namespaces: [] therefore also
clears the watch scope back to cluster-wide.
*/}}
{{- define "kagent.watchNamespaces" -}}
{{- if .Values.controller.watchNamespaces -}}
  {{- .Values.controller.watchNamespaces | uniq | join "," -}}
{{- else -}}
  {{- include "kagent.rbacNamespaces" . | fromJsonArray | join "," -}}
{{- end -}}
{{- end -}}

{{/*
The resolved RBAC scope, as a JSON list so callers can range over it.
Precedence: rbac.namespaces > global.watchNamespaces > empty (cluster-scoped).
The global is a fallback, not an override: a values file that sets rbac.namespaces
renders exactly what it rendered before the global existed.

hasKey, not coalesce: an explicit `rbac.namespaces: []` means "cluster-scoped",
and coalesce would skip it as empty -- silently namespacing an install that
asked not to be. A present key always wins, even empty.

controller.watchNamespaces joins the scope: the controller needs a Role in
every namespace it watches, so a watch entry outside the RBAC list would be a
permanent Forbidden loop. kagent.rbac.validate rejects that mix for an explicit
rbac.namespaces; under the global the watch entries are folded in instead.

The install namespace is appended only on the global path. The global is a
shared signal an umbrella may aim at other charts entirely; failing this
chart's render because that list omits its namespace would brick an install
the value was never about. An explicit rbac.namespaces keeps the hard fail --
there the operator is talking about this chart.
*/}}
{{- define "kagent.rbacNamespaces" -}}
{{- $scope := list -}}
{{- if and .Values.rbac (hasKey .Values.rbac "namespaces") -}}
{{- $scope = .Values.rbac.namespaces | default list -}}
{{- else if ((.Values.global).watchNamespaces) -}}
{{- $scope = concat (.Values.global).watchNamespaces (.Values.controller.watchNamespaces | default list) (list (include "kagent.namespace" .)) -}}
{{- end -}}
{{- $scope | uniq | sortAlpha | toJson -}}
{{- end -}}

{{/*
Guards on the rbac block
*/}}
{{- define "kagent.rbac.validate" -}}
{{- if and .Values.rbac (hasKey .Values.rbac "clusterScoped") -}}
{{- fail "rbac.clusterScoped has been removed. Leave rbac.namespaces empty for cluster-scoped RBAC, or set rbac.namespaces=[<ns>, ...] for namespaced RBAC." -}}
{{- end -}}
{{- $resolved := include "kagent.rbacNamespaces" . | fromJsonArray -}}
{{- if and .Values.rbac .Values.rbac.namespaces -}}
{{- $installNs := include "kagent.namespace" . -}}
{{- if not (has $installNs .Values.rbac.namespaces) -}}
{{- fail (printf "rbac.namespaces is set but does not include the install namespace %q" $installNs) -}}
{{- end -}}
{{/*
A watch wider than the RBAC scope is never valid: the controller lists and
watches namespaces its Roles do not cover, and every reconcile there returns
Forbidden at runtime with only a log line to show for it. Narrower is fine --
an operator may grant Roles broadly and watch a subset to keep the cache small.
*/}}
{{- range $ns := (.Values.controller.watchNamespaces | default list) -}}
{{- if not (has $ns $.Values.rbac.namespaces) -}}
{{- fail (printf "controller.watchNamespaces includes %q but rbac.namespaces does not. The controller would watch a namespace it has no Role in, and every list/watch there returns Forbidden at runtime. Add %q to rbac.namespaces, or remove it from controller.watchNamespaces. Prefer setting only global.watchNamespaces, which scopes RBAC and the watch together." $ns $ns) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Returns "1" when a PodDisruptionBudget threshold is explicitly set, empty otherwise.

Uses `kindIs "invalid"` rather than `default ""` so that an explicit `0` counts as
set: Helm's `default` treats 0 as empty, which would silently drop a
`maxUnavailable: 0` budget and render a manifest the user never asked for.
An empty string is also treated as unset, so `minAvailable: ""` disables the field.
*/}}
{{- define "kagent.pdb.isSet" -}}
{{- if not (kindIs "invalid" .) -}}
{{- if ne (toString .) "" -}}1{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Guards on a component `pdb` block.

Kubernetes rejects a PodDisruptionBudget that sets both `minAvailable` and
`maxUnavailable`, and a budget that sets neither is meaningless, so both cases
fail at template time with a message naming the offending values path rather
than surfacing later as an opaque API server error.

Call with a dict: (dict "pdb" .Values.controller.pdb "path" "controller.pdb")
*/}}
{{- define "kagent.pdb.validate" -}}
{{- $pdb := .pdb | default dict -}}
{{- if $pdb.enabled -}}
{{- $hasMin := include "kagent.pdb.isSet" $pdb.minAvailable -}}
{{- $hasMax := include "kagent.pdb.isSet" $pdb.maxUnavailable -}}
{{- if and $hasMin $hasMax -}}
{{- fail (printf "%s: minAvailable and maxUnavailable are mutually exclusive. Set exactly one (to use minAvailable, set %s.maxUnavailable=null)." .path .path) -}}
{{- end -}}
{{- if not (or $hasMin $hasMax) -}}
{{- fail (printf "%s is enabled but neither minAvailable nor maxUnavailable is set. Set exactly one." .path) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
UI selector labels
*/}}
{{- define "kagent.ui.selectorLabels" -}}
{{ include "kagent.selectorLabels" . }}
app.kubernetes.io/component: ui
{{- end }}

{{/*
Controller selector labels
*/}}
{{- define "kagent.controller.selectorLabels" -}}
{{ include "kagent.selectorLabels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Engine selector labels
*/}}
{{- define "kagent.engine.selectorLabels" -}}
{{ include "kagent.selectorLabels" . }}
app.kubernetes.io/component: engine
{{- end }}

{{/*
Controller labels
*/}}
{{- define "kagent.controller.labels" -}}
{{ include "kagent.labels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
UI labels
*/}}
{{- define "kagent.ui.labels" -}}
{{ include "kagent.labels" . }}
app.kubernetes.io/component: ui
{{- end }}

{{/*
Engine labels
*/}}
{{- define "kagent.engine.labels" -}}
{{ include "kagent.labels" . }}
app.kubernetes.io/component: engine
{{- end }}

{{/*
Check if leader election should be enabled (more than 1 replica)
*/}}
{{- define "kagent.leaderElectionEnabled" -}}
{{- gt (.Values.controller.replicas | int) 1 -}}
{{- end -}}

{{/*
Extract the TCP port from controller.metrics.bindAddress.

Anchors the digit run to the end of the string so every Go-style
address form the controller binary accepts is handled correctly: bare
":port", host-qualified "host:port", and bracketed IPv6 "[::1]:port"
all yield the trailing port. Returns "0" or "" when the binary's
disable sentinel is in use; callers must consult
`kagent.controller.metricsEnabled` before rendering manifests.
*/}}
{{- define "kagent.controller.metricsPort" -}}
{{- regexFind "[0-9]+$" (.Values.controller.metrics.bindAddress | toString) -}}
{{- end -}}

{{/*
Returns "1" when the controller metrics resources (Service, RBAC,
container port, env vars) should render, empty otherwise. Honours both
disable signals: `controller.metrics.enabled=false` and the binary's
own `--metrics-bind-address=0` sentinel reached through `bindAddress`.
The two are equivalent so the field name keeps faith with the binary's
documented contract.
*/}}
{{- define "kagent.controller.metricsEnabled" -}}
{{- $port := include "kagent.controller.metricsPort" . -}}
{{- if and .Values.controller.metrics.enabled $port (ne $port "0") -}}1{{- end -}}
{{- end -}}

{{/*
Controller gRPC observability PrometheusRule name.
*/}}
{{- define "kagent.controller.grpcPrometheusRuleName" -}}
{{- printf "%s-controller-grpc" (include "kagent.fullname" .) -}}
{{- end -}}

{{/*
Controller gRPC observability Grafana dashboard ConfigMap name.
*/}}
{{- define "kagent.controller.grpcDashboardConfigMapName" -}}
{{- printf "%s-controller-grpc-dashboard" (include "kagent.fullname" .) -}}
{{- end -}}

{{/*
PostgreSQL service name for the bundled postgres instance
*/}}
{{- define "kagent.postgresqlServiceName" -}}
{{- printf "%s-postgresql" (include "kagent.fullname" .) -}}
{{- end -}}

{{/*
Bundled PostgreSQL image - constructs the full image reference from registry/repository/name/tag
*/}}
{{- define "kagent.postgresql.image" -}}
{{- $pg := .Values.database.postgres.bundled -}}
{{- $registry := default $pg.image.registry ((.Values.global).imageRegistry) -}}
{{- $parts := compact (list $registry $pg.image.repository $pg.image.name) -}}
{{- printf "%s:%s" (join "/" $parts) $pg.image.tag -}}
{{- end -}}

{{/*
Password secret name - returns the chart-managed Secret name for POSTGRES_PASSWORD.
*/}}
{{- define "kagent.passwordSecretName" -}}
{{- printf "%s-postgresql" (include "kagent.fullname" .) -}}
{{- end -}}

{{/* Public A2A endpoint advertised by AgentInstance Agent Cards. */}}
{{- define "kagent.a2aGatewayUrl" -}}
{{- if .Values.controller.a2aGatewayUrl -}}
{{- .Values.controller.a2aGatewayUrl -}}
{{- else -}}
{{- printf "http://%s-controller.%s.svc:%d" (include "kagent.fullname" .) (include "kagent.namespace" .) (.Values.controller.service.ports.port | int) -}}
{{- end -}}
{{- end -}}

{{/*
Controller Service host:port for nginx upstream (no scheme).
*/}}
{{- define "kagent.controllerServiceAuthority" -}}
{{- printf "%s-controller.%s.svc:%d" (include "kagent.fullname" .) (include "kagent.namespace" .) (.Values.controller.service.ports.port | int) -}}
{{- end -}}

{{/*
imagePullSecrets from global values (for subchart usage).
Reads .Values.global.imagePullSecrets set by the parent chart.
*/}}
{{/*
imagePullSecrets for a pod spec: a component-local list (or the chart-level
one) merged (union) with global.imagePullSecrets. One definition, called from
every pod spec -- the merge written twice drifts, and the pod that misses a
semantics change fails ImagePullBackOff only in the air-gap case the global
exists for.

Usage: {{ include "kagent.imagePullSecrets" (dict "root" $ "local" .Values.controller.imagePullSecrets) }}
*/}}
{{/*
imagePullPolicy for a container: the component's own value, then the chart-level
imagePullPolicy, then global.imagePullPolicy, then IfNotPresent. One definition so
the fallback chain cannot drift between pods.

Usage: {{ include "kagent.imagePullPolicy" (dict "root" $ "local" .Values.controller.image.pullPolicy) }}
*/}}
{{- define "kagent.imagePullPolicy" -}}
{{- .local | default .root.Values.imagePullPolicy | default ((.root.Values.global).imagePullPolicy) | default "IfNotPresent" -}}
{{- end -}}

{{- define "kagent.imagePullSecrets" -}}
{{- $local := .local | default .root.Values.imagePullSecrets | default list -}}
{{- $merged := concat $local (((.root.Values.global).imagePullSecrets) | default list) | uniq -}}
{{- if $merged -}}
imagePullSecrets:
{{- toYaml $merged | nindent 2 }}
{{- end -}}
{{- end -}}

{{/*
Endpoint the controller dials to reach ateapi.

An explicit controller.substrate.ateApiEndpoint always wins. Otherwise, when
substrate is installed as a subchart of this release, its own helper is asked
for the endpoint: the chart prefixes resource names with the release name for
any release not called "substrate", so the Service is not at the canonical
api.ate-system.svc and only the subchart knows what it rendered.

Empty when substrate is not a subchart, which leaves the controller on its
compiled-in default — correct for the topology where substrate is installed as
its own release and the endpoint is passed explicitly.
*/}}
{{- define "kagent.substrate.ateApiEndpoint" -}}
{{- if .Values.controller.substrate.ateApiEndpoint -}}
{{- .Values.controller.substrate.ateApiEndpoint -}}
{{- else if and .Values.substrate .Values.substrate.enabled -}}
{{- include "substrate.ateApi.endpoint" . -}}
{{- end -}}
{{- end -}}

{{/*
URL the controller uses to reach atenet-router, resolved the same way as
kagent.substrate.ateApiEndpoint.
*/}}
{{- define "kagent.substrate.atenetRouterURL" -}}
{{- if .Values.controller.substrate.atenetRouterURL -}}
{{- .Values.controller.substrate.atenetRouterURL -}}
{{- else if and .Values.substrate .Values.substrate.enabled -}}
{{- include "substrate.atenetRouter.url" . -}}
{{- end -}}
{{- end -}}

{{/*
Body of oauth2-proxy's custom sign_in.html template (see
templates/oauth2-proxy-templates.yaml). Kept as its own named template, rather
than inline in that ConfigMap, so oauth2-proxy.extraEnv in values.yaml can hash
the content.

oauth2-proxy renders this as its own Go html/template (not a Helm template) when
it shows the sign-in page to an unauthenticated visitor -- e.g. a request to
/agents/foo is served this page at /oauth2/sign_in?rd=%2Fagents%2Ffoo.
`Redirect` is oauth2-proxy's template variable carrying that original
destination (escaped with a Helm string-literal action so Helm emits it for
oauth2-proxy to evaluate, instead of trying to evaluate it itself). It is
forwarded to kagent's branded /login page.
*/}}
{{- define "kagent.oauth2ProxySignInHTML" -}}
<!DOCTYPE html>
<html>
<head>
  <meta http-equiv="refresh" content="0;url=/login?rd={{ "{{" }} or .Redirect "/" | urlquery {{ "}}" }}">
  <script>window.location.href = "/login?rd={{ "{{" }} or .Redirect "/" | urlquery {{ "}}" }}";</script>
</head>
<body>Redirecting to login...</body>
</html>
{{- end -}}

{{/*
The controller container image. Builds the image root from controller.image and
resolves it through kagent.images.image, so the deployment carries one short
call. The top-level tag wins over the component tag, as it always has.
*/}}
{{- define "kagent.controllerImage" -}}
{{- $root := dict "registry" (.Values.controller.image.registry | default .Values.registry) "repository" .Values.controller.image.repository "tag" (coalesce .Values.tag .Values.controller.image.tag .Chart.Version) -}}
{{- include "kagent.images.image" (dict "imageRoot" $root "global" .Values.global) -}}
{{- end -}}

{{/*
Rewrite a full image reference onto global.imageRegistry, for values that carry
a whole reference in one string rather than registry/repository/tag keys.
Follows the container runtime's rule for deciding whether the first path
segment is a registry: it is one only when it contains a dot or a colon or is
exactly "localhost". A host-carrying reference has that segment replaced so the
mirror sees a stable path; a bare Docker Hub-style name is prefixed instead.
When global.imageRegistry is unset the reference passes through unchanged.
Call with (dict "root" $ "image" <reference>).
*/}}
{{- define "kagent.mirroredImage" -}}
{{- $ref := .image -}}
{{- $mirror := ((.root.Values.global).imageRegistry) -}}
{{- if and $mirror $ref -}}
  {{- $parts := splitList "/" $ref -}}
  {{- $first := first $parts -}}
  {{- if and (gt (len $parts) 1) (or (contains "." $first) (contains ":" $first) (eq $first "localhost")) -}}
    {{- printf "%s/%s" $mirror (join "/" (rest $parts)) -}}
  {{- else -}}
    {{- printf "%s/%s" $mirror $ref -}}
  {{- end -}}
{{- else -}}
  {{- $ref -}}
{{- end -}}
{{- end -}}

{{/*
The ui container image. Same tag precedence as the controller: the top-level
tag wins over the component tag.
*/}}
{{- define "kagent.uiImage" -}}
{{- $root := dict "registry" (.Values.ui.image.registry | default .Values.registry) "repository" .Values.ui.image.repository "tag" (coalesce .Values.tag .Values.ui.image.tag .Chart.Version) -}}
{{- include "kagent.images.image" (dict "imageRoot" $root "global" .Values.global) -}}
{{- end -}}
