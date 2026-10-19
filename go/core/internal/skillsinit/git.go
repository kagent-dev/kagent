package skillsinit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

var immutableGitCommit = regexp.MustCompile(`^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// GatewayAuthorizationPlaceholder is the Authorization header value sent for
// gateway-authorized sources. Substrate's egress gateway replaces only headers
// a request already carries, so the runtime must send one; the gateway
// overwrites the whole value with the referenced Secret.
const GatewayAuthorizationPlaceholder = "kagent-credential-injected"

// CloneGitCommit fetches only one immutable commit instead of cloning the
// repository's complete history. When gatewayAuthorization is set, requests to
// url carry a placeholder Authorization header for the egress gateway to
// replace; the credential itself never reaches this process. A failed or
// cancelled fetch removes destination.
func CloneGitCommit(ctx context.Context, url, commit, destination string, gatewayAuthorization bool) (err error) {
	if !immutableGitCommit.MatchString(commit) {
		return fmt.Errorf("git commit must be a full SHA")
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(destination)
		}
	}()
	if err := runGitIn(ctx, destination, nil, "init", "--quiet"); err != nil {
		return err
	}
	if err := runGitIn(ctx, destination, nil, "remote", "add", "origin", url); err != nil {
		return err
	}
	var config []gitConfig
	if gatewayAuthorization {
		config = gatewayAuthorizationConfig(url)
	}
	if err := runGitIn(ctx, destination, config, "fetch", "--depth", "1", "origin", commit); err != nil {
		return err
	}
	return runGitIn(ctx, destination, nil, "checkout", "--quiet", "--detach", "FETCH_HEAD")
}

type gitConfig struct{ key, value string }

// gatewayAuthorizationConfig scopes the placeholder header to url. Redirects
// are refused because Git keeps sending configured headers after following
// one, which would hand the placeholder to a destination the gateway may also
// authorize, and so let the redirecting server choose who receives the
// credential.
func gatewayAuthorizationConfig(url string) []gitConfig {
	return []gitConfig{
		{key: "http." + url + ".extraHeader", value: "Authorization: " + GatewayAuthorizationPlaceholder},
		{key: "http.followRedirects", value: "false"},
	}
}

// gitCommand builds a git command in dir. config is passed through
// GIT_CONFIG_* for this command only, so nothing is written to .git/config,
// nothing appears in argv, and URLs containing '=' cannot split the key from
// the value.
func gitCommand(ctx context.Context, dir string, config []gitConfig, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// A runtime has no terminal: an authentication failure must fail the
	// fetch instead of waiting for a username prompt.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT="+strconv.Itoa(len(config)))
	for i, entry := range config {
		index := strconv.Itoa(i)
		cmd.Env = append(cmd.Env, "GIT_CONFIG_KEY_"+index+"="+entry.key, "GIT_CONFIG_VALUE_"+index+"="+entry.value)
	}
	return cmd
}

func runGitIn(ctx context.Context, dir string, config []gitConfig, args ...string) error {
	cmd := gitCommand(ctx, dir, config, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("git %s: %w", args[0], ctxErr)
		}
		return fmt.Errorf("git %s: %w", args[0], err)
	}
	return nil
}
