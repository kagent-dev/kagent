package client

type callObservation struct {
	userID      string
	hasDeadline bool
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
