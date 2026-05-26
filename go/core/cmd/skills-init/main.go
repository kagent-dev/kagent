// Command skills-init is the init container binary that fetches an Agent's
// skills from git repositories and OCI images before the main agent container
// starts.
//
// It reads its configuration from a ConfigMap-mounted JSON file (see the
// skillsinit package for the wire format) and performs all subprocess
// invocations with argv vectors — no user input is ever interpolated into a
// shell, which is the original design defect that motivated this rewrite.
package main

import (
	"log"
	"os"

	"github.com/kagent-dev/kagent/go/core/internal/skillsinit"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := skillsinit.LoadConfig()
	if err != nil {
		log.Fatalf("skills-init: %v", err)
	}

	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/root"
	}

	if err := skillsinit.Run(cfg, home); err != nil {
		log.Fatalf("skills-init: %v", err)
	}
}
