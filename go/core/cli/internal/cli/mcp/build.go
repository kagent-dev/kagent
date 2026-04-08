package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/stoewer/go-strcase"

	commonexec "github.com/kagent-dev/kagent/go/core/cli/internal/common/exec"
	commonk8s "github.com/kagent-dev/kagent/go/core/cli/internal/common/k8s"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp/builder"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp/manifests"
)

// BuildCfg contains configuration for MCP build command
type BuildCfg struct {
	Tag             string
	Push            bool
	KindLoad        bool
	ProjectDir      string
	Platform        string
	KindLoadCluster string
}

func BuildMcp(cfg *BuildCfg) error {
	appCfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Determine build directory
	buildDirectory := cfg.ProjectDir
	if buildDirectory == "" {
		var err error
		buildDirectory, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	imageName := cfg.Tag
	if imageName == "" {
		// Load project manifest
		manifestManager := manifests.NewManager(buildDirectory)
		if !manifestManager.Exists() {
			return fmt.Errorf(
				"manifest.yaml not found in %s. Run 'kagent mcp init' first or specify a valid path with --project-dir",
				buildDirectory,
			)
		}

		projectManifest, err := manifestManager.Load()
		if err != nil {
			return fmt.Errorf("failed to load project manifest: %w", err)
		}

		version := projectManifest.Version
		if version == "" {
			version = "latest"
		}
		imageName = fmt.Sprintf("%s:%s", strcase.KebabCase(projectManifest.Name), version)
	}

	// Execute build
	mcpBuilder := builder.New()
	opts := builder.Options{
		ProjectDir: buildDirectory,
		Tag:        imageName,
		Platform:   cfg.Platform,
		Verbose:    appCfg.Verbose,
	}

	if err := mcpBuilder.Build(opts); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	if cfg.Push {
		fmt.Printf("Pushing Docker image %s...\n", imageName)
		docker := commonexec.NewDockerExecutor(appCfg.Verbose, "")
		if err := docker.Push(imageName); err != nil {
			return fmt.Errorf("docker push failed: %w", err)
		}
	}
	if cfg.KindLoad || cfg.KindLoadCluster != "" {
		fmt.Printf("Loading Docker image %s into kind cluster...\n", imageName)
		kindArgs := []string{"load", "docker-image", imageName}
		clusterName := cfg.KindLoadCluster
		if clusterName == "" {
			var err error
			clusterName, err = commonk8s.GetCurrentKindClusterName()
			if err != nil {
				if appCfg.Verbose {
					fmt.Printf("could not detect kind cluster name: %v, using default\n", err)
				}
				clusterName = "kind" // default to kind cluster
			}
		}

		kindArgs = append(kindArgs, "--name", clusterName)

		if err := runKind(kindArgs...); err != nil {
			return fmt.Errorf("kind load failed: %w", err)
		}
		fmt.Printf("✅ Docker image loaded into kind cluster %s\n", clusterName)
	}

	return nil
}

func runKind(args ...string) error {
	cfg, err := config.Get()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}
	if cfg.Verbose {
		fmt.Printf("Running: kind %s\n", strings.Join(args, " "))
	}
	cmd := exec.Command("kind", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
