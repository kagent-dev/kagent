package substrate

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
)

func TestActorTemplateSandboxClass(t *testing.T) {
	for _, tt := range []struct {
		name       string
		class      atev1alpha1.SandboxClass
		wantClass  ateapipb.SandboxClass
		wantConfig string
		wantError  string
	}{
		{name: "default", wantClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR, wantConfig: "gvisor-default"},
		{name: "gvisor", class: atev1alpha1.SandboxClassGvisor, wantClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR, wantConfig: "gvisor-default"},
		{name: "microvm", class: atev1alpha1.SandboxClassMicroVM, wantClass: ateapipb.SandboxClass_SANDBOX_CLASS_MICROVM, wantConfig: "microvm"},
		{name: "unsupported", class: "unsupported", wantError: `unsupported sandbox class "unsupported"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec := &translator.Revision{
				Namespace: "agents", AgentName: "helper",
				WorkerPoolName: "pool",
				AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{Streaming: new(true)},
					SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}},
					DefaultInputModes:   []string{"text"}, DefaultOutputModes: []string{"text"},
				},
			}
			// Exercise builder validation independently of Digest's validation.
			id, err := spec.Digest()
			require.NoError(t, err)
			spec.SandboxClass = tt.class
			template, err := ActorTemplateForRevision(spec, id)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				require.Nil(t, template)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantClass, template.GetSandboxConfig().GetSandboxClass())
			require.Equal(t, tt.wantConfig, template.GetSandboxConfig().GetConfigName())
			testWorkerPoolSelection(t, template, "agents", "pool")
		})
	}
}

// The keys are literals because the Helm chart and externally managed pools
// set them by name; changing a key constant must fail here.
func testWorkerPoolSelection(t *testing.T, template *ateapipb.ActorTemplate, namespace, pool string) {
	t.Helper()
	require.Equal(t, map[string]string{
		"kagent.dev/worker-pool-name":      pool,
		"kagent.dev/worker-pool-namespace": namespace,
	}, template.GetWorkerSelector().GetMatchLabels())
}

// Substrate's ate-api refuses an ActorTemplate whose snapshot_config carries a
// field it does not know, so a removed field (on_pause, tag 1) left on the wire
// keeps every Agent from reaching Ready. The compiled template sends exactly
// on_commit and storage_location.
func testSnapshotConfigWire(t *testing.T, template *ateapipb.ActorTemplate) {
	t.Helper()
	config := template.GetSnapshotConfig()
	require.Empty(t, config.ProtoReflect().GetUnknown())
	wire, err := proto.Marshal(config)
	require.NoError(t, err)
	fields := config.ProtoReflect().Descriptor().Fields()
	var sent []string
	for len(wire) > 0 {
		number, kind, n := protowire.ConsumeTag(wire)
		require.GreaterOrEqual(t, n, 0, "malformed snapshot_config tag")
		wire = wire[n:]
		n = protowire.ConsumeFieldValue(number, kind, wire)
		require.GreaterOrEqual(t, n, 0, "malformed snapshot_config field %d", number)
		wire = wire[n:]
		field := fields.ByNumber(number)
		require.NotNil(t, field, "snapshot_config carries unknown field %d", number)
		sent = append(sent, string(field.Name()))
	}
	require.ElementsMatch(t, []string{"on_commit", "storage_location"}, sent)
}

func TestActorTemplateForRevision(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentName: "helper",
		Image:          "agent.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Command:        []string{"/agent"},
		Args:           []string{"serve"},
		WorkerPoolName: "default", SnapshotLocation: "snapshots",
		ConfigJSON: []byte(`{"instruction":"help"}`), AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{Streaming: new(true)},
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"}},
		Environment:        []corev1.EnvVar{{Name: "API_KEY", Value: translator.CredentialPlaceholder}},
		EgressDestinations: []string{"https://github.com:443", "https://api.anthropic.com:443"},
		Credentials: []egress.Credential{
			{Hostname: "api.anthropic.com", Header: "x-api-key", URI: "ate-secret://k8s.io/default/agents/anthropic/key"},
			{Hostname: "github.com", Header: "authorization", Prefix: "Basic ", URI: "ate-secret://k8s.io/default/agents/skills-git/token", Scope: egress.ScopeGolden},
		},
	}
	revisionID, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	template, err := ActorTemplateForRevision(spec, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if template.GetMetadata().GetAtespace() != "agents" || template.GetMetadata().GetName() != "helper-"+revisionID.Short() {
		t.Fatalf("ActorTemplate = %+v", template)
	}
	container := template.GetContainers()[0]
	if !slices.Equal(container.Command, spec.Command) || !slices.Equal(container.Args, spec.Args) {
		t.Fatalf("container command/args = %v %v", container.Command, container.Args)
	}
	if template.GetSandboxConfig().GetSandboxClass() != ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR || template.GetSandboxConfig().GetConfigName() != "gvisor-default" || container.GetWakeupProbe().GetHttpGet().GetPath() != "/readyz" || container.GetWakeupProbe().GetHttpGet().GetPort() != 8081 || container.GetWakeupProbe().GetTimeoutSeconds() != 30 {
		t.Fatalf("unexpected runtime contract: %+v", template)
	}
	require.Equal(t, ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL, template.GetSnapshotConfig().GetOnCommit())
	testSnapshotConfigWire(t, template)
	environment := map[string]*ateapipb.EnvVar{}
	for _, variable := range container.Env {
		environment[variable.Name] = variable
	}
	for _, name := range []string{"SSL_CERT_FILE", "AWS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE"} {
		if environment[name].Value != egressTrustMount+"/trust-bundle.pem" {
			t.Fatalf("missing gateway trust for %s", name)
		}
	}
	trust := template.Volumes[2].GetSystemInfo().GetDataSources()[0].GetTrustBundle()
	if !slices.Equal(trust.GetNames(), []string{"egress-mitm.ate.dev"}) || trust.GetPath() != "trust-bundle.pem" || container.VolumeMounts[1].GetMountPath() != egressTrustMount {
		t.Fatal("gateway trust bundle was not projected")
	}
	identity := template.Volumes[1].GetSystemInfo().GetDataSources()[0].GetActorMetadata().GetItems()
	if len(identity) != 3 || identity[0].GetField() != ateapipb.ActorMetadataField_ACTOR_METADATA_FIELD_NAME || identity[0].GetPath() != "name" || container.VolumeMounts[2].GetMountPath() != actorIdentityMount {
		t.Fatal("actor routing identity was not projected")
	}
	var rendered a2atype.AgentCard
	if err := json.Unmarshal([]byte(environment["KAGENT_AGENT_CARD_JSON"].Value), &rendered); err != nil {
		t.Fatal(err)
	}
	if rendered.Name != spec.AgentCard.Name || rendered.Description != "" || !rendered.Capabilities.Streaming || len(rendered.Skills) != 0 {
		t.Fatalf("runtime card = %#v", rendered)
	}
	if environment["KAGENT_CONFIG_JSON"].Value != string(spec.ConfigJSON) {
		t.Fatal("config was not embedded as a non-secret literal")
	}
	wantPolicy, err := ActorEgressPolicy("agents", spec.EgressDestinations, spec.Credentials)
	require.NoError(t, err)
	require.True(t, proto.Equal(&ateapipb.EgressPolicyTemplate{Rules: wantPolicy.GetRules()}, template.GetDefaultEgressPolicy()),
		"template default egress policy = %v, want %v", template.GetDefaultEgressPolicy(), wantPolicy.GetRules())
	injected := map[string][]string{}
	for _, rule := range template.GetDefaultEgressPolicy().GetRules() {
		for _, header := range rule.GetHttps().GetEffects().GetReplaceHeaders() {
			injected[rule.GetHttps().GetHostnames()[0]] = append(injected[rule.GetHttps().GetHostnames()[0]], header.GetHeader()+" "+header.GetPrefix()+header.GetCredentialUri())
		}
	}
	require.Equal(t, map[string][]string{
		"api.anthropic.com": {"x-api-key ate-secret://k8s.io/default/agents/anthropic/key"},
		"github.com":        {"authorization Basic ate-secret://k8s.io/default/agents/skills-git/token"},
	}, injected, "the golden boot fetches the private skill source with the artifact binding")
}

func TestActorTemplateForRevisionDeniesAllEgressWithoutDestinations(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentName: "helper",
		Image:          "agent.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkerPoolName: "default", SnapshotLocation: "snapshots",
		ConfigJSON: []byte(`{}`), AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{},
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"}},
	}
	revisionID, err := spec.Digest()
	require.NoError(t, err)
	template, err := ActorTemplateForRevision(spec, revisionID)
	require.NoError(t, err)
	require.NotNil(t, template.GetDefaultEgressPolicy(), "a revision without destinations must carry an empty (deny-all) policy")
	require.Empty(t, template.GetDefaultEgressPolicy().GetRules())
	invalid := *spec
	invalid.EgressDestinations = []string{"github.com"}
	_, err = ActorTemplateForRevision(&invalid, revisionID)
	require.ErrorContains(t, err, "compile egress policy")
}

func TestActorTemplateStampsTheRevisionOnTheResource(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentName: "helper", WorkerPoolName: "default",
		AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{},
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}},
			DefaultInputModes:   []string{"text"}, DefaultOutputModes: []string{"text"}},
		Environment: []corev1.EnvVar{{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "gen_ai.agent.name=helper-kagent,service.version=forged"}},
	}
	revisionID, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	template, err := ActorTemplateForRevision(spec, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, variable := range template.GetContainers()[0].Env {
		if variable.Name == "OTEL_RESOURCE_ATTRIBUTES" {
			if want := "gen_ai.agent.name=helper-kagent,service.version=" + revisionID.Short(); variable.Value != want {
				t.Fatalf("resource attributes = %q, want %q", variable.Value, want)
			}
			if spec.Environment[0].Value != "gen_ai.agent.name=helper-kagent,service.version=forged" {
				t.Fatal("stamping the revision changed the compiled revision")
			}
			return
		}
	}
	t.Fatal("resource attributes missing from the actor")
}

func TestActorTemplateSpecEqualIgnoresServerFields(t *testing.T) {
	left := &ateapipb.ActorTemplate{
		Metadata:   &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "template"},
		Containers: []*ateapipb.Container{{Name: "agent", Image: "agent:v1"}},
	}
	right := proto.CloneOf(left)
	right.Metadata.Uid = "uid"
	right.Metadata.Version = 2
	right.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"}}}
	if !ActorTemplateSpecEqual(left, right) {
		t.Fatal("server-owned fields changed the immutable spec comparison")
	}
	right.Containers[0].Image = "agent:v2"
	if ActorTemplateSpecEqual(left, right) {
		t.Fatal("different container image was accepted")
	}
	right = proto.CloneOf(left)
	right.DefaultEgressPolicy = &ateapipb.EgressPolicyTemplate{Rules: []*ateapipb.EgressRule{{Https: &ateapipb.HTTPSRule{Hostnames: []string{"github.com"}}}}}
	if ActorTemplateSpecEqual(left, right) {
		t.Fatal("different default egress policy was accepted")
	}
}

func TestActorTemplateEnvironmentValueSize(t *testing.T) {
	const configOverhead = len(`{"instruction":""}`)
	for _, test := range []struct {
		name        string
		instruction string
		description string
		environment []corev1.EnvVar
		wantError   string
	}{
		{name: "ASCII boundary", instruction: strings.Repeat("a", 32768-configOverhead)},
		{name: "ASCII overflow", instruction: strings.Repeat("a", 32769-configOverhead), wantError: `environment variable "KAGENT_CONFIG_JSON" is 32769 characters; Substrate supports at most 32768`},
		{name: "Unicode boundary", instruction: strings.Repeat("日", 32768-configOverhead)},
		{name: "Unicode overflow", instruction: strings.Repeat("日", 32769-configOverhead), wantError: `environment variable "KAGENT_CONFIG_JSON" is 32769 characters; Substrate supports at most 32768`},
		{name: "JSON escaping", instruction: strings.Repeat("<>&", 2000), wantError: `environment variable "KAGENT_CONFIG_JSON" is 36018 characters; Substrate supports at most 32768`},
		{name: "user environment", environment: []corev1.EnvVar{{Name: "EXTRA", Value: strings.Repeat("x", 32769)}}, wantError: `environment variable "EXTRA" is 32769 characters; Substrate supports at most 32768`},
		{name: "agent card", description: strings.Repeat("x", 32769), wantError: `environment variable "KAGENT_AGENT_CARD_JSON"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := json.Marshal(struct {
				Instruction string `json:"instruction"`
			}{test.instruction})
			require.NoError(t, err)
			spec := &translator.Revision{
				Namespace: "agents", AgentName: "helper", WorkerPoolName: "default",
				ConfigJSON: config, Environment: test.environment,
				AgentCard: &a2apb.AgentCard{Name: "helper", Description: test.description, Version: "v1", Capabilities: &a2apb.AgentCapabilities{},
					SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}},
					DefaultInputModes:   []string{"text"}, DefaultOutputModes: []string{"text"}},
			}
			id, err := spec.Digest()
			require.NoError(t, err)
			template, err := ActorTemplateForRevision(spec, id)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				require.Nil(t, template)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, template)
		})
	}
}

func TestActorTemplateAddsTheRevisionsCapabilities(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentName: "helper",
		Image:          "agent.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkerPoolName: "default", SnapshotLocation: "snapshots",
		ConfigJSON: []byte(`{}`), AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{},
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"}},
	}
	plainID, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := ActorTemplateForRevision(spec, plainID)
	if err != nil {
		t.Fatal(err)
	}
	if plain.GetContainers()[0].GetSecurityContext() != nil {
		t.Fatalf("a revision without capabilities keeps Substrate's default set, got %v", plain.GetContainers()[0].GetSecurityContext())
	}
	spec.Capabilities = []string{"SETUID", "SETGID"}
	id, err := spec.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if id == plainID {
		t.Fatal("the capabilities are part of the revision identity")
	}
	template, err := ActorTemplateForRevision(spec, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := template.GetContainers()[0].GetSecurityContext().GetCapabilities().GetAdd(); !slices.Equal(got, []string{"SETUID", "SETGID"}) {
		t.Fatalf("added capabilities = %v", got)
	}
}
