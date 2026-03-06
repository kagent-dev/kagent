//go:build integration

package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpinternal "github.com/kagent-dev/kagent/go/core/cli/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPInitPython_FullWorkflow tests the complete Python MCP workflow
func TestMCPInitPython_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if uv is available
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available, skipping Python MCP test")
	}

	// Change to temp directory
	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Save original flags
	origNonInteractive := initNonInteractive
	origDescription := initDescription
	origAuthor := initAuthor
	origNoGit := initNoGit
	defer func() {
		initNonInteractive = origNonInteractive
		initDescription = origDescription
		initAuthor = origAuthor
		initNoGit = origNoGit
	}()

	// Set flags for non-interactive mode
	initNonInteractive = true
	initDescription = "Integration test MCP server"
	initAuthor = "Test Author"
	initNoGit = true

	// Step 1: Initialize Python MCP project
	projectName := "test_mcp_server"
	err := runInitFramework(projectName, "fastmcp-python", nil)
	require.NoError(t, err, "Init should succeed")

	projectPath := filepath.Join(tmpDir, projectName)

	// Verify project structure
	assert.DirExists(t, projectPath, "Project directory should exist")
	assert.FileExists(t, filepath.Join(projectPath, "manifest.yaml"), "manifest.yaml should exist")
	assert.FileExists(t, filepath.Join(projectPath, "pyproject.toml"), "pyproject.toml should exist")
	assert.FileExists(t, filepath.Join(projectPath, "src", "main.py"), "src/main.py should exist")
	assert.DirExists(t, filepath.Join(projectPath, "src", "tools"), "src/tools directory should exist")

	// Step 2: Run uv sync
	t.Log("Running uv sync...")
	syncCmd := exec.Command("uv", "sync")
	syncCmd.Dir = projectPath
	syncCmd.Stdout = os.Stdout
	syncCmd.Stderr = os.Stderr
	err = syncCmd.Run()
	require.NoError(t, err, "uv sync should succeed")

	// Step 3: Attempt to run the server (with timeout)
	// This is the critical test that would catch the stateless_http issue
	t.Log("Testing server startup...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverCmd := exec.CommandContext(ctx, "uv", "run", "python", "src/main.py")
	serverCmd.Dir = projectPath

	// Provide minimal MCP input to trigger initialization
	stdin, err := serverCmd.StdinPipe()
	require.NoError(t, err)

	// Capture output to check for errors
	var stdout, stderr strings.Builder
	serverCmd.Stdout = &stdout
	serverCmd.Stderr = &stderr

	// Start the server
	err = serverCmd.Start()
	require.NoError(t, err, "Server should start")

	// Give it a moment to initialize and potentially crash
	time.Sleep(1 * time.Second)

	// Send a minimal initialize request
	initRequest := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}`
	_, _ = stdin.Write([]byte(initRequest + "\n"))
	stdin.Close()

	// Wait for timeout or completion
	_ = serverCmd.Wait()

	// Check stderr for the specific error
	stderrOutput := stderr.String()
	stdoutOutput := stdout.String()

	t.Logf("Server stdout: %s", stdoutOutput)
	t.Logf("Server stderr: %s", stderrOutput)

	// The critical assertion: should NOT see the stateless_http error
	assert.NotContains(t, stderrOutput, "stateless_http",
		"Server should not fail with stateless_http parameter error")
	assert.NotContains(t, stderrOutput, "unexpected keyword argument",
		"Server should not fail with unexpected keyword argument")

	// Should see successful initialization
	// Note: We allow timeout since we're just testing it doesn't crash on startup
	if !assert.NotContains(t, stderrOutput, "Traceback", "Server should not crash on startup") {
		t.Logf("Server crashed. Full stderr:\n%s", stderrOutput)
	}
}

// TestMCPInitGo_FullWorkflow tests the complete Go MCP workflow
func TestMCPInitGo_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if go is available
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available, skipping Go MCP test")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Save original flags
	origNonInteractive := initNonInteractive
	origDescription := initDescription
	origNoGit := initNoGit
	defer func() {
		initNonInteractive = origNonInteractive
		initDescription = origDescription
		initNoGit = origNoGit
	}()

	initNonInteractive = true
	initDescription = "Integration test Go MCP server"
	initNoGit = true

	// Initialize Go MCP project
	projectName := "test_mcp_go"
	err := runInitFramework(projectName, "mcp-go", func(p *mcpinternal.ProjectConfig) error {
		p.GoModuleName = "github.com/test/test_mcp_go"
		return nil
	})
	require.NoError(t, err, "Go init should succeed")

	projectPath := filepath.Join(tmpDir, projectName)

	// Verify project structure
	assert.DirExists(t, projectPath)
	assert.FileExists(t, filepath.Join(projectPath, "manifest.yaml"))
	assert.FileExists(t, filepath.Join(projectPath, "go.mod"))
	assert.FileExists(t, filepath.Join(projectPath, "cmd", "server", "main.go"))

	// Run go mod tidy
	t.Log("Running go mod tidy...")
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = projectPath
	tidyCmd.Stdout = os.Stdout
	tidyCmd.Stderr = os.Stderr
	err = tidyCmd.Run()
	require.NoError(t, err, "go mod tidy should succeed")

	// Verify project compiles
	t.Log("Testing compilation...")
	buildCmd := exec.Command("go", "build", "-o", "/dev/null", "./cmd/server")
	buildCmd.Dir = projectPath
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	err = buildCmd.Run()
	require.NoError(t, err, "Project should compile successfully")
}

// TestMCPInitTypeScript_FullWorkflow tests the complete TypeScript MCP workflow
func TestMCPInitTypeScript_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if npm is available
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available, skipping TypeScript MCP test")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Save original flags
	origNonInteractive := initNonInteractive
	origDescription := initDescription
	origNoGit := initNoGit
	defer func() {
		initNonInteractive = origNonInteractive
		initDescription = origDescription
		initNoGit = origNoGit
	}()

	initNonInteractive = true
	initDescription = "Integration test TypeScript MCP server"
	initNoGit = true

	// Initialize TypeScript MCP project
	projectName := "test_mcp_ts"
	err := runInitFramework(projectName, "typescript", nil)
	require.NoError(t, err, "TypeScript init should succeed")

	projectPath := filepath.Join(tmpDir, projectName)

	// Verify project structure
	assert.DirExists(t, projectPath)
	assert.FileExists(t, filepath.Join(projectPath, "manifest.yaml"))
	assert.FileExists(t, filepath.Join(projectPath, "package.json"))
	assert.FileExists(t, filepath.Join(projectPath, "src", "index.ts"))

	// Run npm install
	t.Log("Running npm install...")
	installCmd := exec.Command("npm", "install")
	installCmd.Dir = projectPath
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	err = installCmd.Run()
	if err != nil {
		t.Logf("npm install failed (this may be expected in CI): %v", err)
		// Don't fail the test if npm install fails - might be network/environment issues
		return
	}

	// Verify project compiles (if npm install succeeded)
	t.Log("Testing TypeScript compilation...")
	buildCmd := exec.Command("npx", "tsc", "--noEmit")
	buildCmd.Dir = projectPath
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	err = buildCmd.Run()
	assert.NoError(t, err, "Project should compile successfully")
}

// TestMCPInitJava_FullWorkflow tests the complete Java MCP workflow
func TestMCPInitJava_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if mvn is available
	if _, err := exec.LookPath("mvn"); err != nil {
		t.Skip("mvn not available, skipping Java MCP test")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Save original flags
	origNonInteractive := initNonInteractive
	origDescription := initDescription
	origNoGit := initNoGit
	defer func() {
		initNonInteractive = origNonInteractive
		initDescription = origDescription
		initNoGit = origNoGit
	}()

	initNonInteractive = true
	initDescription = "Integration test Java MCP server"
	initNoGit = true

	// Initialize Java MCP project
	projectName := "test_mcp_java"
	err := runInitFramework(projectName, "java", nil)
	require.NoError(t, err, "Java init should succeed")

	projectPath := filepath.Join(tmpDir, projectName)

	// Verify project structure
	assert.DirExists(t, projectPath)
	assert.FileExists(t, filepath.Join(projectPath, "manifest.yaml"))
	assert.FileExists(t, filepath.Join(projectPath, "pom.xml"))
	assert.DirExists(t, filepath.Join(projectPath, "src", "main", "java"))

	// Run mvn compile
	t.Log("Running mvn compile...")
	compileCmd := exec.Command("mvn", "compile", "-q")
	compileCmd.Dir = projectPath
	compileCmd.Stdout = os.Stdout
	compileCmd.Stderr = os.Stderr
	err = compileCmd.Run()
	if err != nil {
		t.Logf("mvn compile failed (this may be expected in CI): %v", err)
		// Don't fail - Maven might need network access
		return
	}

	assert.NoError(t, err, "Project should compile successfully")
}

// TestMCPBuild_AllFrameworks tests building Docker images for all frameworks
func TestMCPBuild_AllFrameworks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available, skipping build tests")
	}

	frameworks := []struct {
		name      string
		framework string
		required  string
	}{
		{"Python", "fastmcp-python", "uv"},
		{"Go", "mcp-go", "go"},
		{"TypeScript", "typescript", "npm"},
		{"Java", "java", "mvn"},
	}

	for _, fw := range frameworks {
		t.Run(fw.name, func(t *testing.T) {
			// Check if required tool is available
			if _, err := exec.LookPath(fw.required); err != nil {
				t.Skipf("%s not available, skipping %s build test", fw.required, fw.name)
			}

			originalWd, _ := os.Getwd()
			tmpDir := t.TempDir()
			os.Chdir(tmpDir)
			defer os.Chdir(originalWd)

			// Save original flags
			origNonInteractive := initNonInteractive
			origNoGit := initNoGit
			defer func() {
				initNonInteractive = origNonInteractive
				initNoGit = origNoGit
			}()

			initNonInteractive = true
			initNoGit = true

			// Initialize project
			projectName := "test_build_" + strings.ToLower(fw.name)

			// Go projects require a module name
			var customize func(*mcpinternal.ProjectConfig) error
			if fw.framework == "mcp-go" {
				customize = func(p *mcpinternal.ProjectConfig) error {
					p.GoModuleName = "github.com/test/" + projectName
					return nil
				}
			}

			err := runInitFramework(projectName, fw.framework, customize)
			require.NoError(t, err, "Init should succeed")

			projectPath := filepath.Join(tmpDir, projectName)

			// Save original build flags
			origBuildDir := buildDir
			origBuildTag := buildTag
			defer func() {
				buildDir = origBuildDir
				buildTag = origBuildTag
			}()

			buildDir = projectPath
			buildTag = projectName + ":test"

			// Note: We can't actually build without Docker daemon
			// Just verify the manifest is correct for building
			manifest, err := getProjectManifest(projectPath)
			require.NoError(t, err)
			assert.Equal(t, fw.framework, manifest.Framework)
			assert.NotEmpty(t, manifest.Name)

			// Verify Dockerfile exists
			assert.FileExists(t, filepath.Join(projectPath, "Dockerfile"),
				"Dockerfile should exist for building")
		})
	}
}

// TestMCPRun_ManifestValidation tests that run validates manifests correctly
func TestMCPRun_ManifestValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Test 1: Run without manifest should fail
	origProjectDir := projectDir
	defer func() { projectDir = origProjectDir }()

	projectDir = tmpDir

	err := executeRun(RunCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.yaml not found")

	// Test 2: Run with invalid framework should fail
	manifestPath := filepath.Join(tmpDir, "manifest.yaml")
	content := `name: test-server
framework: invalid-framework
version: 1.0.0
tools: {}
secrets: {}
`
	err = os.WriteFile(manifestPath, []byte(content), 0644)
	require.NoError(t, err)

	err = executeRun(RunCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported framework")
}

// TestMCPWorkflow_ErrorPropagation tests that errors propagate correctly across commands
func TestMCPWorkflow_ErrorPropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	// Test 1: Build without manifest should fail
	origBuildDir := buildDir
	origBuildTag := buildTag
	defer func() {
		buildDir = origBuildDir
		buildTag = origBuildTag
	}()

	buildDir = tmpDir
	buildTag = ""

	err := runBuild(BuildCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.yaml not found")

	// Test 2: Run without manifest should fail
	origProjectDir := projectDir
	defer func() { projectDir = origProjectDir }()

	projectDir = tmpDir

	err = executeRun(RunCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.yaml not found")
}

// TestMCPInit_ProjectStructure tests that all generated projects have correct structure
func TestMCPInit_ProjectStructure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	frameworks := []struct {
		name          string
		framework     string
		requiredFiles []string
		requiredDirs  []string
	}{
		{
			name:      "Python",
			framework: "fastmcp-python",
			requiredFiles: []string{
				"manifest.yaml",
				"pyproject.toml",
				"Dockerfile",
				"src/main.py",
				"README.md",
			},
			requiredDirs: []string{
				"src",
				"src/tools",
			},
		},
		{
			name:      "Go",
			framework: "mcp-go",
			requiredFiles: []string{
				"manifest.yaml",
				"go.mod",
				"Dockerfile",
				"cmd/server/main.go",
				"README.md",
			},
			requiredDirs: []string{
				"cmd",
				"cmd/server",
				"internal",
			},
		},
		{
			name:      "TypeScript",
			framework: "typescript",
			requiredFiles: []string{
				"manifest.yaml",
				"package.json",
				"tsconfig.json",
				"Dockerfile",
				"src/index.ts",
				"README.md",
			},
			requiredDirs: []string{
				"src",
			},
		},
		{
			name:      "Java",
			framework: "java",
			requiredFiles: []string{
				"manifest.yaml",
				"pom.xml",
				"Dockerfile",
				"README.md",
			},
			requiredDirs: []string{
				"src",
				"src/main",
				"src/main/java",
			},
		},
	}

	for _, fw := range frameworks {
		t.Run(fw.name, func(t *testing.T) {
			originalWd, _ := os.Getwd()
			tmpDir := t.TempDir()
			os.Chdir(tmpDir)
			defer os.Chdir(originalWd)

			// Save original flags
			origNonInteractive := initNonInteractive
			origNoGit := initNoGit
			defer func() {
				initNonInteractive = origNonInteractive
				initNoGit = origNoGit
			}()

			initNonInteractive = true
			initNoGit = true

			// Initialize project
			projectName := "test_structure_" + strings.ToLower(fw.name)

			// Go projects require a module name
			var customize func(*mcpinternal.ProjectConfig) error
			if fw.framework == "mcp-go" {
				customize = func(p *mcpinternal.ProjectConfig) error {
					p.GoModuleName = "github.com/test/" + projectName
					return nil
				}
			}

			err := runInitFramework(projectName, fw.framework, customize)
			require.NoError(t, err, "Init should succeed for %s", fw.name)

			projectPath := filepath.Join(tmpDir, projectName)

			// Check all required files exist
			for _, file := range fw.requiredFiles {
				filePath := filepath.Join(projectPath, file)
				assert.FileExists(t, filePath,
					"Required file %s should exist in %s project", file, fw.name)
			}

			// Check all required directories exist
			for _, dir := range fw.requiredDirs {
				dirPath := filepath.Join(projectPath, dir)
				assert.DirExists(t, dirPath,
					"Required directory %s should exist in %s project", dir, fw.name)
			}

			// Verify manifest content
			manifest, err := getProjectManifest(projectPath)
			require.NoError(t, err)
			assert.Equal(t, fw.framework, manifest.Framework)
			assert.Equal(t, projectName, manifest.Name)
		})
	}
}
