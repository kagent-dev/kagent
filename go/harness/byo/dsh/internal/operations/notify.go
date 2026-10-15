package operations

import (
	"context"
	"errors"
	"fmt"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

type A2ANotifier struct {
	config    Continuation
	transport a2aclient.Transport
}

var _ Notifier = (*A2ANotifier)(nil)

var errNotAccepted = errors.New("continuation was not accepted")

func NewA2ANotifier(config Continuation, token string) *A2ANotifier {
	return &A2ANotifier{config: config, transport: a2aclient.NewJSONRPCTransport(config.URL, authenticatedClient(token, 30*time.Second))}
}

func (a *A2ANotifier) Notify(ctx context.Context, o Operation) error {
	// No old TaskID: DSH continues in a new A2A task within the bound context.
	// Send only an opaque identifier and state. The model fetches the durable
	// result via get_operation instead of trusting a payload as instructions.
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(fmt.Sprintf("External operation %s reached state %s. Query get_operation with that operation_id to read its saved result. Do not resubmit this operation.", o.ID, o.State)))
	message.ID = "operation-" + o.ID
	message.ContextID = a.config.ContextID
	// Acknowledgement is enough: waiting for a model turn would turn ordinary
	// long-running continuations into ambiguous delivery timeouts.
	_, err := a.transport.SendMessage(ctx, nil, &a2atype.SendMessageRequest{
		Message: message,
		Config:  &a2atype.SendMessageConfig{ReturnImmediately: true},
	})
	// Only this controller contract proves that no task/message was accepted.
	// Timeouts, connection failures and generic HTTP errors remain ambiguous.
	var protocolError *a2atype.Error
	if errors.Is(err, a2atype.ErrUnsupportedOperation) && errors.As(err, &protocolError) {
		info := protocolError.ErrorInfo().Value
		metadata, _ := info["metadata"].(map[string]string)
		if info["domain"] == a2atype.ProtocolDomain && metadata["reason"] == "KAGENT_SEND_NOT_ACCEPTED" {
			return errNotAccepted
		}
	}
	return err
}
