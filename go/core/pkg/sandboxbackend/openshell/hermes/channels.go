package hermes

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/channels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type messagingState struct {
	resolved *channels.Resolved
}

// AccumulateMessagingChannels resolves channel credentials and returns messaging state for Hermes bootstrap.
func AccumulateMessagingChannels(ctx context.Context, kube client.Client, namespace string, specChannels []v1alpha2.AgentHarnessChannel, _ map[string]string) (*messagingState, error) {
	resolved, err := channels.Resolve(ctx, kube, namespace, specChannels)
	if err != nil {
		return nil, err
	}
	return &messagingState{resolved: resolved}, nil
}

func (st *messagingState) hasTelegram() bool {
	return st != nil && st.resolved != nil && st.resolved.HasTelegram
}

func (st *messagingState) hasSlack() bool {
	return st != nil && st.resolved != nil && st.resolved.HasSlack
}

func (st *messagingState) telegramAllow() []string {
	if st == nil || st.resolved == nil {
		return nil
	}
	return st.resolved.TelegramAllow
}

func (st *messagingState) slackAllow() []string {
	if st == nil || st.resolved == nil {
		return nil
	}
	return st.resolved.SlackAllow
}

func (st *messagingState) slackHomeChannel() string {
	if st == nil || st.resolved == nil {
		return ""
	}
	return st.resolved.SlackHomeChannel
}

func (st *messagingState) slackHomeChannelName() string {
	if st == nil || st.resolved == nil {
		return ""
	}
	return st.resolved.SlackHomeChannelName
}

func (st *messagingState) secrets() map[string]string {
	if st == nil || st.resolved == nil {
		return nil
	}
	return st.resolved.Secrets
}
