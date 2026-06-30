package substrate

import (
	"context"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func envByName(envs []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
	for _, e := range envs {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

func TestBuildHermesChannelEnvTelegram(t *testing.T) {
	chs := []v1alpha2.AgentHarnessChannel{{
		Name: "tg",
		Type: v1alpha2.AgentHarnessChannelTypeTelegram,
		Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{
			BotToken:       v1alpha2.AgentHarnessChannelCredential{Value: "tg-token"},
			AllowedUserIDs: []string{"1", "2"},
		},
	}}
	env, err := buildHermesChannelEnv(context.Background(), nil, "ns", chs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e, ok := envByName(env, "TELEGRAM_BOT_TOKEN"); !ok || e.Value != "tg-token" {
		t.Fatalf("missing/incorrect TELEGRAM_BOT_TOKEN: %+v", env)
	}
	if e, ok := envByName(env, "TELEGRAM_ALLOWED_USERS"); !ok || e.Value != "1,2" {
		t.Fatalf("missing/incorrect TELEGRAM_ALLOWED_USERS: %+v", env)
	}
}

func TestBuildHermesChannelEnvSlackFromSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "allow", Namespace: "ns"},
		Data:       map[string][]byte{"users": []byte("U1,U2")},
	}).Build()

	chs := []v1alpha2.AgentHarnessChannel{{
		Name: "sl",
		Type: v1alpha2.AgentHarnessChannelTypeSlack,
		Slack: &v1alpha2.AgentHarnessSlackChannelSpec{
			BotToken: v1alpha2.AgentHarnessChannelCredential{
				ValueFrom: &v1alpha2.ValueSource{Type: v1alpha2.SecretValueSource, Name: "slack", Key: "bot"},
			},
			AppToken: v1alpha2.AgentHarnessChannelCredential{Value: "xapp-1"},
			Hermes: &v1alpha2.AgentHarnessHermesSlackOptions{
				AllowedUserIDsFrom: &v1alpha2.ValueSource{Type: v1alpha2.SecretValueSource, Name: "allow", Key: "users"},
				HomeChannel:        "C123",
				HomeChannelName:    "general",
			},
		},
	}}
	env, err := buildHermesChannelEnv(context.Background(), kube, "ns", chs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bot, ok := envByName(env, "SLACK_BOT_TOKEN")
	if !ok || bot.ValueFrom == nil || bot.ValueFrom.SecretKeyRef == nil || bot.ValueFrom.SecretKeyRef.Name != "slack" {
		t.Fatalf("SLACK_BOT_TOKEN should be a secret ref: %+v", bot)
	}
	if e, ok := envByName(env, "SLACK_APP_TOKEN"); !ok || e.Value != "xapp-1" {
		t.Fatalf("missing/incorrect SLACK_APP_TOKEN: %+v", env)
	}
	if e, ok := envByName(env, "SLACK_ALLOWED_USERS"); !ok || e.Value != "U1,U2" {
		t.Fatalf("missing/incorrect SLACK_ALLOWED_USERS: %+v", env)
	}
	if e, ok := envByName(env, "SLACK_HOME_CHANNEL"); !ok || e.Value != "C123" {
		t.Fatalf("missing/incorrect SLACK_HOME_CHANNEL: %+v", env)
	}
	if e, ok := envByName(env, "SLACK_HOME_CHANNEL_NAME"); !ok || e.Value != "general" {
		t.Fatalf("missing/incorrect SLACK_HOME_CHANNEL_NAME: %+v", env)
	}
}

func TestBuildHermesChannelEnvDuplicateType(t *testing.T) {
	chs := []v1alpha2.AgentHarnessChannel{
		{Name: "a", Type: v1alpha2.AgentHarnessChannelTypeTelegram, Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "x"}}},
		{Name: "b", Type: v1alpha2.AgentHarnessChannelTypeTelegram, Telegram: &v1alpha2.AgentHarnessTelegramChannelSpec{BotToken: v1alpha2.AgentHarnessChannelCredential{Value: "y"}}},
	}
	if _, err := buildHermesChannelEnv(context.Background(), nil, "ns", chs); err == nil {
		t.Fatal("expected error for duplicate telegram channel")
	}
}

func TestBuildHermesChannelEnvUnsupportedType(t *testing.T) {
	chs := []v1alpha2.AgentHarnessChannel{{Name: "x", Type: v1alpha2.AgentHarnessChannelType("discord")}}
	if _, err := buildHermesChannelEnv(context.Background(), nil, "ns", chs); err == nil {
		t.Fatal("expected error for unsupported channel type")
	}
}

func TestBuildAcpStartupScript(t *testing.T) {
	child := []string{"hermes", "acp"}

	noGateway := buildAcpStartupScript("", child, false)
	if strings.Contains(noGateway, "hermes-gateway-ensure.sh") {
		t.Fatalf("non-gateway script should not reference gateway: %q", noGateway)
	}
	if !strings.Contains(noGateway, "exec /usr/local/bin/acp-shim") {
		t.Fatalf("expected acp-shim exec: %q", noGateway)
	}
	if !strings.HasSuffix(noGateway, "-- hermes acp") {
		t.Fatalf("expected child appended: %q", noGateway)
	}

	gateway := buildAcpStartupScript("export FOO=bar\n", child, true)
	if !strings.Contains(gateway, "export FOO=bar") {
		t.Fatalf("expected prelude preserved: %q", gateway)
	}
	// The gateway is launched only inside the acp-shim child wrapper (on the
	// first post-restore connection), NOT pre-warmed before acp-shim — a
	// snapshot-baked gateway restores with dead platform connections. So
	// hermes-gateway-ensure.sh must appear exactly once, inside the child shell.
	if strings.Count(gateway, "hermes-gateway-ensure.sh") != 1 {
		t.Fatalf("expected exactly one (child-wrapper) gateway ensure, not a pre-warm: %q", gateway)
	}
	if strings.Contains(gateway, "hermes-gateway-ensure.sh || true\nexec /usr/local/bin/acp-shim") {
		t.Fatalf("gateway must not be pre-warmed before acp-shim: %q", gateway)
	}
	if !strings.Contains(gateway, "exec hermes acp") {
		t.Fatalf("expected child exec inside shell: %q", gateway)
	}
}
