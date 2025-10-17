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

// PromptForConfirmation asks a yes/no question and returns true if the user confirms.
// Accepts "y", "yes" (case-insensitive) as confirmation.
func PromptForConfirmation(question string) (bool, error) {
	response, err := PromptForInput(question + " (y/n): ")
	if err != nil {
		return false, err
	}

	response = strings.ToLower(response)
	return response == "y" || response == "yes", nil
}

// PromptWithDefault displays a prompt with a default value and reads user input.
// If the user presses Enter without typing anything, the default value is returned.
func PromptWithDefault(promptText, defaultValue string) (string, error) {
	prompt := fmt.Sprintf("%s [%s]: ", promptText, defaultValue)
	input, err := PromptForInput(prompt)
	if err != nil {
		return "", err
	}

	if input == "" {
		return defaultValue, nil
	}

	return input, nil
}

// PromptForSelection displays a list of options and prompts the user to select one.
// Returns the index of the selected option (0-based) or an error.
func PromptForSelection(promptText string, options []string) (int, error) {
	fmt.Println(promptText)
	for i, option := range options {
		fmt.Printf("%d. %s\n", i+1, option)
	}

	for {
		input, err := PromptForInput("Enter selection (1-" + fmt.Sprintf("%d", len(options)) + "): ")
		if err != nil {
			return 0, err
		}

		var selection int
		if _, err := fmt.Sscanf(input, "%d", &selection); err != nil {
			fmt.Println("Invalid input. Please enter a number.")
			continue
		}

		if selection < 1 || selection > len(options) {
			fmt.Printf("Invalid selection. Please enter a number between 1 and %d.\n", len(options))
			continue
		}

		return selection - 1, nil
	}
}
