package env

// SSHConnection is set by sshd; the TUI then copies through the terminal rather than the remote machine's clipboard.
var SSHConnection = RegisterStringVar("SSH_CONNECTION", "", "Set by sshd. The TUI then copies selected text through the terminal (OSC 52) as well as the remote machine's clipboard tools.", ComponentCLI)

// ClaudeConfigDir is Claude Code's configuration directory; kagent skill install links the skill under it.
var ClaudeConfigDir = RegisterStringVar("CLAUDE_CONFIG_DIR", "", "Claude Code's configuration directory, ~/.claude when unset. kagent skill install links the kagent skill into its skills directory.", ComponentCLI)
