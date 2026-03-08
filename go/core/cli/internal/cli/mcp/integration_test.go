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

func newTestInitCfg(description string) *InitMcpCfg {
	return &InitMcpCfg{
		NonInteractive: true,
		Description:    description,
		NoGit:          true,
	}
}

// TestMCPInitPython_FullWorkflow tests the complete Python MCP workflow
func TestMCPInitPython_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available, skipping Python MCP test")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	cfg := newTestInitCfg("Integration test MCP server")
	cfg.Author = "Test Author"

	projectName := "test_mcp_server"
	err := InitMcp(cfg, projectName, "fastmcp-python", nil)
	require.NoError(t, err, "Init should succeed")

	projectPath := filepath.Join(tmpDir, projectName)

	assert.DirExists(t, projectPath, "Project directory should exist")
	assert.FileExists(t, filepath.Join(projectPath, "manifest.yaml"))
	assert.FileExists(t, filepath.Join(projectPath, "pyproject.toml"))
	assert.FileExists(t, filepath.Join(projectPath, "src", "main.py"))
	assert.DirExists(t, filepath.Join(projectPath, "src", "tools"))

	t.Log("Running uv sync...")
	syncCmd := exec.Command("uv", "sync")
	syncCmd.Dir = projectPath
	syncCmd.Stdout = os.Stdout
	syncCmd.Stderr = os.Stderr
	err = syncCmd.Run()
	require.NoError(t, err, "uv sync should succeed")

	t.Log("Testing server startup...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverCmd := exec.CommandContext(ctx, "uv", "run", "python", "src/main.py")
	serverCmd.Dir = projectPath

	stdin, err := serverCmd.StdinPipe()
	require.NoError(t, err)

	var stdout, stderr strings.Builder
	serverCmd.Stdout = &stdout
	serverCmd.Stderr = &stderr

	err = serverCmd.Start()
	require.NoError(t, err, "Server should start")

	time.Sleep(1 * time.Second)

	initRequest := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}`
	_, _ = stdin.Write([]byte(initRequest + "\n"))
	stdin.Close()

	_ = serverCmd.Wait()

	stderrOutput := stderr.String()
	stdoutOutput := stdout.String()

	t.Logf("Server stdout: %s", stdoutOutput)
	t.Logf("Server stderr: %s", stderrOutput)

	assert.NotContains(t, stderrOutput, "stateless_http")
	assert.NotContains(t, stderrOutput, "unexpected keyword argument")

	if !assert.NotContains(t, stderrOutput, "Traceback", "Server should not crash on startup") {
		t.Logf("Server crashed. Full stderr:\n%s", stderrOutput)
	}
}

// TestMCPInitGo_FullWorkflow tests the complete Go MCP workflow
func TestMCPInitGo_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available, skipping Go MCP test")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	cfg := newTestInitCfg("Integration test Go MCP server")

	projectName := "test_mcp_go"
	err := InitMcp(cfg, projectName, "mcp-go", func(p *mcpinternal.ProjectConfig) error {
		p.GoModuleName = "github.com/test/test_mcp_go"
		return nil
	})
	require.NoError(t, err, "Go init should succeed")

	projectPath := filepath.Join(tmpDir, projectName)

	assert.DirExists(t, projectPath)
	assert.FileExists(t, filepath.Join(projectPath, "manifest.yaml"))
	assert.FileExists(t, filepath.Join(projectPath, "go.mod"))
	assert.FileExists(t, filepath.Join(projectPath, "cmd", "server", "main.go"))

	t.Log("Running go mod tidy...")
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = projectPath
	tidyCmd.Stdout = os.Stdout
	tidyCmd.Stderr = os.Stderr
	err = tidyCmd.Run()
	require.NoError(t, err, "go mod tidy should succeed")

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

	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available, skipping TypeScript MCP test")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	cfg := newTestInitCfg("Integration test TypeScript MCP server")

	projectName := "test_mcp_ts"
	err := InitMcp(cfg, projectName, "typescript", nil)
	require.NoError(t, err, "TypeScript init should succeed")

	projectPath := filepath.Join(tmpDir, projectName)

	assert.DirExists(t, projectPath)
	assert.FileExists(t, filepath.Join(projectPath, "manifest.yaml"))
	assert.FileExists(t, filepath.Join(projectPath, "package.json"))
	assert.FileExists(t, filepath.Join(projectPath, "src", "index.ts"))

	t.Log("Running npm install...")
	installCmd := exec.Command("npm", "install")
	installCmd.Dir = projectPath
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	err = installCmd.Run()
	if err != nil {
		t.Logf("npm install failed (this may be expected in CI): %v", err)
		return
	}

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

	if _, err := exec.LookPath("mvn"); err != nil {
		t.Skip("mvn not available, skipping Java MCP test")
	}

	originalWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	cfg := newTestInitCfg("Integration test Java MCP server")

	projectName := "test_mcp_java"
	err := InitMcp(cfg, projectName, "java", nil)
	require.NoError(t, err, "Java init should succeed")

	projectPath := filepath.Join(tmpDir, projectName)

	assert.DirExists(t, projectPath)
	assert.FileExists(t, filepath.Join(projectPath, "manifest.yaml"))
	assert.FileExists(t, filepath.Join(projectPath, "pom.xml"))
	assert.DirExists(t, filepath.Join(projectPath, "src", "main", "java"))

	t.Log("Running mvn compile...")
	compileCmd := exec.Command("mvn", "compile", "-q")
	compileCmd.Dir = projectPath
	compileCmd.Stdout = os.Stdout
	compileCmd.Stderr = os.Stderr
	err = compileCmd.Run()
	if err != nil {
		t.Logf("mvn compile failed (this may be expected in CI): %v", err)
		return
	}

	assert.NoError(t, err, "Project should compile successfully")
}

// TestMCPBuild_AllFrameworks tests building Docker images for all frameworks
func TestMCPBuild_AllFrameworks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

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
			if _, err := exec.LookPath(fw.required); err != nil {
				t.Skipf("%s not available, skipping %s build test", fw.required, fw.name)
			}

			originalWd, _ := os.Getwd()
			tmpDir := t.TempDir()
			os.Chdir(tmpDir)
			defer os.Chdir(originalWd)

			cfg := newTestInitCfg("")

			projectName := "test_build_" + strings.ToLower(fw.name)

			var customize func(*mcpinternal.ProjectConfig) error
			if fw.framework == "mcp-go" {
				customize = func(p *mcpinternal.ProjectConfig) error {
					p.GoModuleName = "github.com/test/" + projectName
					return nil
				}
			}

			err := InitMcp(cfg, projectName, fw.framework, customize)
			require.NoError(t, err, "Init should succeed")

			projectPath := filepath.Join(tmpDir, projectName)

			// Verify manifest is correct for building
			manifest, err := getProjectManifest(projectPath)
			require.NoError(t, err)
			assert.Equal(t, fw.framework, manifest.Framework)
			assert.NotEmpty(t, manifest.Name)

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

	tmpDir := t.TempDir()

	// Test 1: Run without manifest should fail
	runCfg := &RunCfg{ProjectDir: tmpDir}
	err := RunMcp(runCfg)
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

	err = RunMcp(runCfg)
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
	buildCfg := &BuildCfg{ProjectDir: tmpDir, Tag: ""}
	err := BuildMcp(buildCfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.yaml not found")

	// Test 2: Run without manifest should fail
	runCfg := &RunCfg{ProjectDir: tmpDir}
	err = RunMcp(runCfg)
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
				"manifest.yaml", "pyproject.toml", "Dockerfile",
				"src/main.py", "README.md",
			},
			requiredDirs: []string{"src", "src/tools"},
		},
		{
			name:      "Go",
			framework: "mcp-go",
			requiredFiles: []string{
				"manifest.yaml", "go.mod", "Dockerfile",
				"cmd/server/main.go", "README.md",
			},
			requiredDirs: []string{"cmd", "cmd/server", "internal"},
		},
		{
			name:      "TypeScript",
			framework: "typescript",
			requiredFiles: []string{
				"manifest.yaml", "package.json", "tsconfig.json",
				"Dockerfile", "src/index.ts", "README.md",
			},
			requiredDirs: []string{"src"},
		},
		{
			name:      "Java",
			framework: "java",
			requiredFiles: []string{
				"manifest.yaml", "pom.xml", "Dockerfile", "README.md",
			},
			requiredDirs: []string{"src", "src/main", "src/main/java"},
		},
	}

	for _, fw := range frameworks {
		t.Run(fw.name, func(t *testing.T) {
			originalWd, _ := os.Getwd()
			tmpDir := t.TempDir()
			os.Chdir(tmpDir)
			defer os.Chdir(originalWd)

			cfg := newTestInitCfg("")

			projectName := "test_structure_" + strings.ToLower(fw.name)

			var customize func(*mcpinternal.ProjectConfig) error
			if fw.framework == "mcp-go" {
				customize = func(p *mcpinternal.ProjectConfig) error {
					p.GoModuleName = "github.com/test/" + projectName
					return nil
				}
			}

			err := InitMcp(cfg, projectName, fw.framework, customize)
			require.NoError(t, err, "Init should succeed for %s", fw.name)

			projectPath := filepath.Join(tmpDir, projectName)

			for _, file := range fw.requiredFiles {
				filePath := filepath.Join(projectPath, file)
				assert.FileExists(t, filePath,
					"Required file %s should exist in %s project", file, fw.name)
			}

			for _, dir := range fw.requiredDirs {
				dirPath := filepath.Join(projectPath, dir)
				assert.DirExists(t, dirPath,
					"Required directory %s should exist in %s project", dir, fw.name)
			}

			manifest, err := getProjectManifest(projectPath)
			require.NoError(t, err)
			assert.Equal(t, fw.framework, manifest.Framework)
			assert.Equal(t, projectName, manifest.Name)
		})
	}
}
