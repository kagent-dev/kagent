package driver

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// The bounds a turn ends at with a cap result, as clients read them under
// kagent.dev/a2a/stop-reason.
const (
	StopReasonTurnTimeLimit          = "turn_time_limit"
	StopReasonCallerCredentialExpiry = "caller_credential_expiry"
)

// callerCredentialExpiryMargin ends a turn this long before the caller's
// bearer expires, so the last MCP call Claude makes with it still carries a
// token the server accepts, clock skew between the two included.
const callerCredentialExpiryMargin = 30 * time.Second

// turnBound is the time a turn segment ends at and the cap result it ends with.
type turnBound struct {
	at   time.Time
	stop runtime.Stop
}

// boundTurn returns the earlier of a segment's time limit, counted from start,
// and the caller's bearer expiry less callerCredentialExpiryMargin; a zero
// timeout or expiry sets no bound of its own.
func boundTurn(start time.Time, timeout time.Duration, credentialExpiry time.Time) (turnBound, bool) {
	var bound turnBound
	bounded := false
	if timeout > 0 {
		bound = turnBound{at: start.Add(timeout), stop: runtime.Stop{
			Reason: StopReasonTurnTimeLimit,
			Message: fmt.Sprintf("The turn reached its time limit of %s and was stopped. "+
				"Send \"continue\" to carry on from where it stopped.", timeout),
		}}
		bounded = true
	}
	if !credentialExpiry.IsZero() {
		at := credentialExpiry.Add(-callerCredentialExpiryMargin)
		if !bounded || at.Before(bound.at) {
			bound = turnBound{at: at, stop: runtime.Stop{
				Reason: StopReasonCallerCredentialExpiry,
				Message: fmt.Sprintf("The turn was stopped because your sign-in expires at %s. "+
					"Send \"continue\" to carry on from where it stopped with a fresh sign-in.",
					credentialExpiry.UTC().Format("15:04 MST")),
			}}
			bounded = true
		}
	}
	return bound, bounded
}

// bearerExpiry reads the exp claim of a JWT bearer credential. It verifies
// nothing: the token only bounds the turn, and the MCP server that receives it
// is the one that verifies it. An opaque token, another scheme or a token
// without exp has no expiry.
func bearerExpiry(credential string) (time.Time, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(credential), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return time.Time{}, false
	}
	segments := strings.Split(strings.TrimSpace(token), ".")
	if len(segments) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segments[1], "="))
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp *float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == nil || *claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(*claims.Exp), 0), true
}
