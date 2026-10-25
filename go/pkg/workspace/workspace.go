// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

// Package workspace owns repository initialization and its durable receipt.
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// StartupTimeout bounds checkout and fits inside the platform wakeup probe.
const StartupTimeout = 4 * time.Minute

const stateDirectory = ".kagent-workspace"

var commitPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

type receipt struct {
	Version int    `json:"version"`
	Input   string `json:"input"`
	Commit  string `json:"commit"`
}

// Path is the repository root shared by a runtime and its subagents.
func Path(dataDir string) string { return filepath.Join(dataDir, "workspace") }

// Validate checks source syntax without contacting Git or reading credentials.
func Validate(config *configv1.GitWorkspaceConfig) error {
	if config == nil {
		return nil
	}
	if err := protovalidate.Validate(config); err != nil {
		return fmt.Errorf("invalid workspace source: %w", err)
	}
	u, err := url.Parse(config.Url)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(config.Url, "\\\x00\r\n#") {
		return fmt.Errorf("workspace URL must be HTTPS without credentials, a query, or a fragment")
	}
	for part := range strings.SplitSeq(config.Ref, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") || strings.Contains(part, "..") {
			return fmt.Errorf("workspace ref must be a branch, tag, or full commit")
		}
	}
	return nil
}

// Initialize prepares a fresh workspace once. Completion is stored outside the
// checkout: later edits, commits, or removal of .git never cause a new fetch.
// The caller invokes this once during startup, before serving runtime traffic.
func Initialize(ctx context.Context, dataDir string, config *configv1.GitWorkspaceConfig) error {
	if config == nil {
		return nil
	}
	if err := Validate(config); err != nil {
		return err
	}
	if !filepath.IsAbs(dataDir) {
		return fmt.Errorf("workspace data directory must be absolute")
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode workspace input: %w", err)
	}
	digest := sha256.Sum256(raw)
	input := hex.EncodeToString(digest[:])
	state := filepath.Join(dataDir, stateDirectory)
	if err := ensureDirectory(state); err != nil {
		return err
	}
	complete := filepath.Join(state, "complete.json")
	if _, err := readReceipt(complete, input); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	prepared := filepath.Join(state, "prepared.json")
	ctx, cancel := context.WithTimeout(ctx, StartupTimeout)
	defer cancel()
	staging := filepath.Join(state, "staging")
	destination := Path(dataDir)
	if _, err := readReceipt(prepared, input); errors.Is(err, os.ErrNotExist) {
		if err := requireEmpty(destination); err != nil {
			return err
		}
		// This path is owned exclusively by unfinished initialization. Never
		// remove the published checkout when recovering a partial fetch.
		if err := os.RemoveAll(staging); err != nil {
			return fmt.Errorf("remove incomplete workspace staging: %w", err)
		}
		if err := os.Mkdir(staging, 0o755); err != nil {
			return fmt.Errorf("create workspace staging: %w", err)
		}
		commit, err := checkout(ctx, staging, config)
		if err != nil {
			return err
		}
		data, err := json.Marshal(receipt{Version: 1, Input: input, Commit: commit})
		if err != nil {
			return fmt.Errorf("encode workspace receipt: %w", err)
		}
		if err := os.WriteFile(prepared+".tmp", data, 0o600); err != nil {
			return fmt.Errorf("write workspace receipt: %w", err)
		}
		if err := os.Rename(prepared+".tmp", prepared); err != nil {
			return fmt.Errorf("publish workspace receipt: %w", err)
		}
	} else if err != nil {
		return err
	}
	if info, err := os.Lstat(staging); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("workspace staging must be a directory, not a symlink")
		}
		if err := verifyPreparedCommit(ctx, staging, prepared, input); err != nil {
			return err
		}
		if err := requireEmpty(destination); err != nil {
			return err
		}
		if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove empty workspace directory: %w", err)
		}
		if err := os.Rename(staging, destination); err != nil {
			return fmt.Errorf("publish workspace checkout: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect workspace staging: %w", err)
	} else {
		// A crash may have interrupted completion after the directory rename.
		if err := verifyPreparedCommit(ctx, destination, prepared, input); err != nil {
			return err
		}
	}
	if err := os.Rename(prepared, complete); err != nil {
		return fmt.Errorf("finish workspace initialization: %w", err)
	}
	return nil
}

func verifyPreparedCommit(ctx context.Context, directory, prepared, input string) error {
	saved, err := readReceipt(prepared, input)
	if err != nil {
		return err
	}
	head, err := git(ctx, directory, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("verify unfinished workspace: %w", err)
	}
	if strings.TrimSpace(string(head)) != saved.Commit {
		return fmt.Errorf("workspace does not match unfinished initialization")
	}
	return nil
}

func ensureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create workspace state directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect workspace state: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace state must be a directory, not a symlink")
	}
	return nil
}

func readReceipt(path, input string) (receipt, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return receipt{}, fmt.Errorf("inspect workspace receipt: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 1024 {
		return receipt{}, fmt.Errorf("workspace receipt must be a small regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return receipt{}, fmt.Errorf("read workspace receipt: %w", err)
	}
	var value receipt
	if err := json.Unmarshal(data, &value); err != nil {
		return receipt{}, fmt.Errorf("decode workspace receipt: %w", err)
	}
	if value.Version != 1 || value.Input != input || !commitPattern.MatchString(value.Commit) {
		return receipt{}, fmt.Errorf("workspace receipt conflicts with the configured source")
	}
	return value, nil
}

func requireEmpty(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("uninitialized workspace must be an empty directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read workspace directory: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("refusing to replace a nonempty workspace without an initialization receipt")
	}
	return nil
}

func checkout(ctx context.Context, directory string, config *configv1.GitWorkspaceConfig) (string, error) {
	for _, args := range [][]string{{"init", "--quiet"}, {"remote", "add", "origin", config.Url}} {
		if _, err := git(ctx, directory, args...); err != nil {
			return "", err
		}
	}
	args := []string{"-c", "http.followRedirects=false"}
	if config.Authorization {
		args = append(args, "-c", "http.extraHeader=Authorization: kagent-credential-injected")
	}
	// Fetch the selected ref directly; clone --branch does not accept commits.
	args = append(args, "fetch", "--quiet", "--depth=1", "origin", config.Ref)
	if _, err := git(ctx, directory, args...); err != nil {
		return "", fmt.Errorf("fetch workspace ref: %w", err)
	}
	if _, err := git(ctx, directory, "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
		return "", err
	}
	head, err := git(ctx, directory, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(head))
	if !commitPattern.MatchString(commit) {
		return "", fmt.Errorf("git returned an invalid workspace commit")
	}
	if commitPattern.MatchString(config.Ref) && config.Ref != commit {
		return "", fmt.Errorf("git returned a different commit than requested")
	}
	return commit, nil
}

func git(ctx context.Context, directory string, args ...string) ([]byte, error) {
	args = append([]string{"-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "init.templateDir=", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always"}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = directory
	// Inherit process plumbing and projected TLS trust, never caller-supplied
	// Git configuration, hooks, credential helpers, or repository overrides.
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if !strings.HasPrefix(name, "GIT_") || name == "GIT_SSL_CAINFO" || name == "GIT_SSL_CAPATH" {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_LFS_SKIP_SMUDGE=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("workspace Git command failed: %w", err)
	}
	return output, nil
}
