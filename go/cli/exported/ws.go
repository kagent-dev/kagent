package exported

import "github.com/kagent-dev/kagent/go/cli/internal/ws"

type Config = ws.Config
type Client = ws.Client

var DefaultConfig = ws.DefaultConfig()

func NewWebsocketClient(wsURL string, runID int, config Config) (*Client, error) {
	return ws.NewClient(wsURL, runID, config)
}
