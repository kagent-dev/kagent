package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	commonexec "github.com/kagent-dev/kagent/go/cli/internal/common/exec"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func BugReportCmd(cfg *config.Config) {
	// Create a temporary directory for bug report
	timestamp := time.Now().Format("20060102-150405")
	reportDir := fmt.Sprintf("kagent-bug-report-%s", timestamp)
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating report directory: %v\n", err)
		return
	}

	fmt.Println("Gathering bug report information...")
	kubectl := commonexec.NewKubectlExecutor(cfg.Verbose, cfg.Namespace)

	// Get Agent, ModelConfig, and ToolServers YAMLs
	resources := []string{"agent", "modelconfig", "toolserver", "mcpserver", "remotemcpserver"}
	for _, resource := range resources {
		output, err := kubectl.RunWithOutput("get", resource, "-n", cfg.Namespace, "-o", "yaml")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting %s resources: %v\n", resource, err)
			continue
		}

		filename := filepath.Join(reportDir, fmt.Sprintf("%s.yaml", resource))
		if err := os.WriteFile(filename, output, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s file: %v\n", resource, err)
			continue
		}
	}

	// Get secret names (without values)
	output, err := kubectl.RunWithOutput("get", "secrets", "-n", cfg.Namespace, "-o", "custom-columns=NAME:.metadata.name")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting secret names: %v\n", err)
	} else {
		filename := filepath.Join(reportDir, "secrets.txt")
		if err := os.WriteFile(filename, output, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing secrets file: %v\n", err)
		}
	}

	// Get pod logs
	output, err = kubectl.RunWithOutput("get", "pods", "-n", cfg.Namespace, "-o", "name")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting pod names: %v\n", err)
	} else {
		pods := strings.Split(string(output), "\n")
		for _, pod := range pods {
			if pod == "" {
				continue
			}
			podName := strings.TrimPrefix(pod, "pod/")

			// Get container names for this pod
			containerOutput, err := kubectl.RunWithOutput("get", "pod", podName, "-n", cfg.Namespace, "-o", "jsonpath='{.spec.containers[*].name}'")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting containers for pod %s: %v\n", podName, err)
				continue
			}

			// Parse container names
			containerStr := strings.Trim(string(containerOutput), "'")
			containers := strings.Fields(containerStr)

			if len(containers) == 0 {
				// Fallback to getting logs without specifying container
				logs, err := kubectl.RunWithOutput("logs", "-n", cfg.Namespace, podName)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error getting logs for pod %s: %v\n", podName, err)
					continue
				}

				filename := filepath.Join(reportDir, fmt.Sprintf("%s-logs.txt", podName))
				if err := os.WriteFile(filename, logs, 0644); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing logs for pod %s: %v\n", podName, err)
				}
			} else {
				// Get logs for each container
				for _, container := range containers {
					logs, err := kubectl.RunWithOutput("logs", "-n", cfg.Namespace, podName, "-c", container)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error getting logs for container %s in pod %s: %v\n", container, podName, err)
						continue
					}

					filename := filepath.Join(reportDir, fmt.Sprintf("%s-%s-logs.txt", podName, container))
					if err := os.WriteFile(filename, logs, 0644); err != nil {
						fmt.Fprintf(os.Stderr, "Error writing logs for container %s in pod %s: %v\n", container, podName, err)
					}
				}
			}
		}
	}

	// Get versions and images
	output, err = kubectl.RunWithOutput("get", "pods", "-n", cfg.Namespace, "-o", "jsonpath='{range .items[*]}{.metadata.name}{\"\\n\"}{range .spec.containers[*]}{.image}{\"\\n\"}{end}{end}'")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting pod images: %v\n", err)
	} else {
		filename := filepath.Join(reportDir, "versions.txt")
		if err := os.WriteFile(filename, output, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing versions file: %v\n", err)
		}
	}

	fmt.Printf("Bug report generated in directory: %s\n", reportDir)
	fmt.Println("WARNING: Please review and scrub any sensitive information from agent.yaml before sharing the bug report.")
}
