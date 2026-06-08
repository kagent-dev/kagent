package hermes

// DefaultSSHLaunchCommand is the interactive CLI started when connecting to a
// Hermes harness sandbox via the UI terminal (unless plain shell is requested).
func DefaultSSHLaunchCommand() string {
	// Hermes reads config from HERMES_HOME; bootstrap writes to /sandbox/.hermes.
	return "cd " + HermesConfigDir + " && exec hermes"
}
