// Package prompt provides utilities for interactive user input.
package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// PromptForInput displays a prompt and reads user input from stdin.
// Returns the trimmed input string or an error if reading fails.
func PromptForInput(promptText string) (string, error) {
	fmt.Print(promptText)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return strings.TrimSpace(input), nil
}
