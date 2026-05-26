package hermes

import (
	"bytes"
	_ "embed"
	"text/template"
)

//go:embed gateway_wait.sh
var gatewayListenWaitTemplateSource string

var gatewayListenWaitTemplate = template.Must(template.New("gateway_wait.sh").Parse(gatewayListenWaitTemplateSource))

// GatewayListenWaitScript returns a shell snippet that exits 0 once 127.0.0.1:port is
// listening, exit 2 if ss is unavailable, or exit 1 on timeout with diagnostics.
func GatewayListenWaitScript(port int) string {
	var out bytes.Buffer
	if err := gatewayListenWaitTemplate.Execute(&out, struct{ Port int }{Port: port}); err != nil {
		panic(err)
	}
	return out.String()
}
