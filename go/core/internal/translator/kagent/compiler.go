package kagent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/internal/translator/adkconfig"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
)

// Compiler translates resolved inputs into a kagent runtime revision.
type Compiler struct {
	config *adkconfig.Builder
}

var _ v2translator.HarnessCompiler = (*Compiler)(nil)

func NewCompiler(ctx krt.HandlerContext, collections v2translator.Collections) *Compiler {
	return &Compiler{config: adkconfig.NewBuilder(ctx, collections)}
}

func (c *Compiler) Compile(ctx context.Context, input *v2translator.HarnessInput) (*v2translator.CompileResult, error) {
	telemetryConfig, _ := v2translator.TelemetryConfigFromProcess()
	compiled, err := c.config.Build(ctx, input)
	if err != nil {
		return nil, err
	}
	template, harness := input.Root.Template, input.Harness
	configJSON, err := json.Marshal(compiled.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal agent config: %w", err)
	}
	card, err := pbconv.ToProtoAgentCard(v2translator.ManagedAgentCard(input.AgentName, template))
	if err != nil {
		return nil, fmt.Errorf("convert agent card: %w", err)
	}

	harnessAttributes := v2translator.HarnessResourceAttributes(harness)
	harnessEnvironment := slices.DeleteFunc(adkconfig.HarnessEnvironment(harness), func(variable corev1.EnvVar) bool {
		return v2translator.OwnsTelemetryEnvironment(variable.Name)
	})
	environment := append(compiled.Environment, harnessEnvironment...)
	environment = append(environment,
		corev1.EnvVar{Name: env.KagentName.Name(), Value: input.AgentName},
		corev1.EnvVar{Name: env.KagentNamespace.Name(), Value: template.Namespace},
		corev1.EnvVar{Name: env.KagentAPIURL.Name(), Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace())},
		corev1.EnvVar{Name: env.KagentGatewayURL.Name(), Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace())},
		corev1.EnvVar{Name: env.KagentPort.Name(), Value: "80"},
	)
	environment = append(environment, telemetryConfig.TelemetryEnvironment(tracing.RuntimeTelemetry{
		AgentName: input.AgentName, AgentNamespace: template.Namespace,
	}, harnessAttributes)...)
	environment = adkconfig.DedupeEnv(environment)
	provenance, err := c.config.BuildProvenance(ctx, harness, compiled.Templates, compiled.Models, environment)
	if err != nil {
		return nil, fmt.Errorf("build revision provenance: %w", err)
	}
	environment, credentials, err := v2translator.CompileCredentials(input, compiled.Models, environment)
	if err != nil {
		return nil, err
	}
	compiled.Egress = append(compiled.Egress, telemetryConfig.Destinations()...)
	compiled.Egress = append(compiled.Egress, "http://"+utils.GetControllerName()+"."+utils.GetResourceNamespace()+":8083")
	slices.Sort(compiled.Egress)
	compiled.Egress = slices.Compact(compiled.Egress)
	return &v2translator.CompileResult{Revision: v2translator.Revision{
		Namespace: template.Namespace,
		Image:     harness.Spec.Workload.Image, Command: slices.Clone(harness.Spec.Workload.Command), Args: slices.Clone(harness.Spec.Workload.Args),
		Environment: environment, ConfigJSON: configJSON, AgentCard: card,
		WorkerPoolName: harness.Spec.Substrate.WorkerPoolRef.Name, SnapshotLocation: harness.Spec.Substrate.SnapshotPolicy.Location,
		Credentials: credentials, Provenance: provenance, EgressDestinations: compiled.Egress,
	}}, nil
}
