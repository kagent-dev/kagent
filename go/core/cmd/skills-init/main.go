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
	"os/user"

	"github.com/kagent-dev/kagent/go/core/internal/skillsinit"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := skillsinit.LoadConfig()
	if err != nil {
		log.Fatalf("skills-init: %v", err)
	}

	// Resolve home from /etc/passwd via the current uid rather than $HOME so
	// the binary behaves consistently regardless of how the container was
	// invoked. The image's user is created with a fixed home dir, so this is
	// deterministic in production and overridable in tests via the user db.
	u, err := user.Current()
	if err != nil {
		log.Fatalf("skills-init: lookup current user: %v", err)
	}
	home := u.HomeDir
	if home == "" {
		log.Fatalf("skills-init: current user %q has no home directory", u.Username)
	}

	if err := skillsinit.Run(cfg, home); err != nil {
		log.Fatalf("skills-init: %v", err)
	}
}
