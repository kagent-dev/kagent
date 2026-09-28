// Package session renders Session control-plane fields for the terminal.
package session

import (
	"fmt"
	"strings"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const statePrefix = "RUNTIME_STATE_"

// shortIDLength tells sessions apart in a narrow column; the full ID stays copyable elsewhere.
const shortIDLength = 8

// ShortID abbreviates a Session ID for space-constrained display.
func ShortID(id string) string {
	if len(id) <= shortIDLength {
		return id
	}
	return id[:shortIDLength]
}

// StateLabel renders a lifecycle state without its protobuf enum prefix.
func StateLabel(state apiv1alpha1.RuntimeState) string {
	return strings.TrimPrefix(state.String(), statePrefix)
}

// Ready reports whether a Session can serve A2A calls; the gateway rejects every other state.
func Ready(session *apiv1alpha1.Session) bool {
	return session.GetState() == apiv1alpha1.RuntimeState_RUNTIME_STATE_READY
}

// Age renders elapsed time; an absent timestamp renders empty rather than as the Unix epoch.
func Age(created *timestamppb.Timestamp, now time.Time) string {
	if !created.IsValid() {
		return ""
	}
	return Since(created.AsTime(), now)
}

// Since matches kubectl's age column: the coarsest informative unit.
func Since(t, now time.Time) string {
	elapsed := max(now.Sub(t), 0) // clock skew must not read as a future age
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
	}
}
