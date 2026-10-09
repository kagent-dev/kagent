//go:build !unix

package turn

// Without Unix process groups a command is not started in a group of its own,
// so there is nothing to end beyond the command itself.
func killGroup(int) {}

func groupAlive(int) bool { return false }
