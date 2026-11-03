package env

// SSHConnection is set by sshd; the TUI then copies through the terminal rather than the remote machine's clipboard.
var SSHConnection = RegisterStringVar("SSH_CONNECTION", "", "Set by sshd. The TUI then copies selected text through the terminal (OSC 52) as well as the remote machine's clipboard tools.", ComponentCLI)
