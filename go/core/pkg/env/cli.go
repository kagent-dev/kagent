package env

// TUIBell rings the terminal bell when a turn ends or waits on the user while the TUI's window is unfocused.
var TUIBell = RegisterBoolVar("KAGENT_TUI_BELL", true, "Ring the terminal bell when a TUI turn finishes or waits for input while the terminal window is unfocused. Needs a terminal that reports focus; false turns it off.", ComponentCLI)

// SSHConnection is set by sshd; the TUI then copies through the terminal rather than the remote machine's clipboard.
var SSHConnection = RegisterStringVar("SSH_CONNECTION", "", "Set by sshd. The TUI then copies selected text through the terminal (OSC 52) as well as the remote machine's clipboard tools.", ComponentCLI)
