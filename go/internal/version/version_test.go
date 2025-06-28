package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()

	// Check that all fields are populated
	if info.Version == "" {
		t.Error("Version should not be empty")
	}
	if info.GitCommit == "" {
		t.Error("GitCommit should not be empty")
	}
	if info.BuildDate == "" {
		t.Error("BuildDate should not be empty")
	}
	if info.GoVersion == "" {
		t.Error("GoVersion should not be empty")
	}
	if info.Compiler == "" {
		t.Error("Compiler should not be empty")
	}
	if info.Platform == "" {
		t.Error("Platform should not be empty")
	}

	// Check default values
	if info.Version != Version {
		t.Errorf("Expected Version %s, got %s", Version, info.Version)
	}
	if info.GitCommit != GitCommit {
		t.Errorf("Expected GitCommit %s, got %s", GitCommit, info.GitCommit)
	}
	if info.BuildDate != BuildDate {
		t.Errorf("Expected BuildDate %s, got %s", BuildDate, info.BuildDate)
	}

	// Check runtime values
	if info.GoVersion != runtime.Version() {
		t.Errorf("Expected GoVersion %s, got %s", runtime.Version(), info.GoVersion)
	}
	if info.Compiler != runtime.Compiler {
		t.Errorf("Expected Compiler %s, got %s", runtime.Compiler, info.Compiler)
	}
	expectedPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != expectedPlatform {
		t.Errorf("Expected Platform %s, got %s", expectedPlatform, info.Platform)
	}
}

func TestInfoString(t *testing.T) {
	info := Get()
	str := info.String()

	// Check that all expected values are in the string
	expectedValues := []string{
		info.Version,
		info.GitCommit,
		info.BuildDate,
		info.GoVersion,
		info.Compiler,
		info.Platform,
	}

	for _, value := range expectedValues {
		if !strings.Contains(str, value) {
			t.Errorf("String() output should contain %s, got: %s", value, str)
		}
	}
}

func TestInfoShort(t *testing.T) {
	info := Get()
	short := info.Short()

	// Check format: v{version} ({commit})
	if !strings.Contains(short, info.Version) {
		t.Errorf("Short() should contain version %s, got: %s", info.Version, short)
	}
	if !strings.Contains(short, info.GitCommit) {
		t.Errorf("Short() should contain git commit %s, got: %s", info.GitCommit, short)
	}
	if !strings.HasPrefix(short, "v") {
		t.Errorf("Short() should start with 'v', got: %s", short)
	}
}

func TestGetVersion(t *testing.T) {
	version := GetVersion()
	if version != Version {
		t.Errorf("Expected %s, got %s", Version, version)
	}
}

func TestGetShort(t *testing.T) {
	short := GetShort()
	expected := Get().Short()
	if short != expected {
		t.Errorf("Expected %s, got %s", expected, short)
	}
}

func TestGetFull(t *testing.T) {
	full := GetFull()
	expected := Get().String()
	if full != expected {
		t.Errorf("Expected %s, got %s", expected, full)
	}
}

func TestVersionVariablesModification(t *testing.T) {
	// Save original values
	origVersion := Version
	origGitCommit := GitCommit
	origBuildDate := BuildDate

	// Modify values (simulating build-time injection)
	Version = "1.2.3"
	GitCommit = "abc123def"
	BuildDate = "2024-01-15T10:30:00Z"

	// Test that Get() reflects the changes
	info := Get()
	if info.Version != "1.2.3" {
		t.Errorf("Expected Version 1.2.3, got %s", info.Version)
	}
	if info.GitCommit != "abc123def" {
		t.Errorf("Expected GitCommit abc123def, got %s", info.GitCommit)
	}
	if info.BuildDate != "2024-01-15T10:30:00Z" {
		t.Errorf("Expected BuildDate 2024-01-15T10:30:00Z, got %s", info.BuildDate)
	}

	// Restore original values
	Version = origVersion
	GitCommit = origGitCommit
	BuildDate = origBuildDate
}

func TestDefaultValues(t *testing.T) {
	// Test default values
	if Version != "dev" {
		t.Errorf("Expected default Version 'dev', got %s", Version)
	}
	if GitCommit != "none" {
		t.Errorf("Expected default GitCommit 'none', got %s", GitCommit)
	}
	if BuildDate != "unknown" {
		t.Errorf("Expected default BuildDate 'unknown', got %s", BuildDate)
	}
}
