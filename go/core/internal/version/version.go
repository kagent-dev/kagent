package version

import (
	"fmt"
	"runtime"
)

var (
	// These variables should be set during build time using -ldflags
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

// Info contains version information
type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"gitCommit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Compiler  string `json:"compiler"`
	Platform  string `json:"platform"`
}

// Get returns version information
func Get() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String returns a human-readable version string
func (i Info) String() string {
	return fmt.Sprintf("Version: %s\nGit Commit: %s\nBuild Date: %s\nGo Version: %s\nCompiler: %s\nPlatform: %s",
		i.Version, i.GitCommit, i.BuildDate, i.GoVersion, i.Compiler, i.Platform)
}

// Short returns a short version string
func (i Info) Short() string {
	return fmt.Sprintf("v%s (%s)", i.Version, i.GitCommit)
}

// GetVersion returns just the version string
func GetVersion() string {
	return Version
}

// GetShort returns a short version string using the package variables
func GetShort() string {
	return Get().Short()
}

// GetFull returns the full version information as a string
func GetFull() string {
	return Get().String()
}
