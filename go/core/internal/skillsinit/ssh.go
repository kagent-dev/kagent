package skillsinit

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// SetupGitAuth prepares ~/.ssh and credential helpers from the mounted auth
// secret. If a ssh-privatekey is present, it is copied into place with strict
// permissions and known_hosts is seeded via ssh-keyscan. If a token is
// present, a git credential helper is configured.
//
// homeDir is normally the binary process's $HOME. We accept it explicitly so
// tests can pass a tmpdir.
func SetupGitAuth(homeDir, authMountPath string, hosts []SSHHost) error {
	if authMountPath == "" {
		return nil
	}

	keyPath := filepath.Join(authMountPath, "ssh-privatekey")
	tokenPath := filepath.Join(authMountPath, "token")

	switch {
	case fileExists(keyPath):
		return setupSSHKey(homeDir, keyPath, hosts)
	case fileExists(tokenPath):
		return setupTokenHelper(tokenPath)
	}
	return nil
}

func setupSSHKey(homeDir, keyPath string, hosts []SSHHost) error {
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("mkdir ~/.ssh: %w", err)
	}
	if err := copyFile(keyPath, filepath.Join(sshDir, "id_rsa"), 0o600); err != nil {
		return fmt.Errorf("install ssh key: %w", err)
	}
	knownHosts := filepath.Join(sshDir, "known_hosts")
	if err := touchFile(knownHosts, 0o600); err != nil {
		return fmt.Errorf("touch known_hosts: %w", err)
	}
	for _, h := range hosts {
		if err := keyscan(h, knownHosts); err != nil {
			return fmt.Errorf("ssh-keyscan %s: %w", h.Host, err)
		}
	}
	return nil
}

func setupTokenHelper(tokenPath string) error {
	// Use an absolute path inside the helper string. The helper is invoked
	// by git via /bin/sh, so we keep the body small and quote the literal
	// path — the path itself comes from us, not from user input.
	helper := fmt.Sprintf("!f() { echo username=x-access-token; echo password=$(cat %q); }; f", tokenPath)
	cmd := exec.Command("git", "config", "--global", "credential.helper", helper)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// keyscan invokes ssh-keyscan with an argv vector. Host and Port reach the
// process as separate arguments — they never pass through a shell.
func keyscan(h SSHHost, knownHosts string) error {
	args := []string{"-H"}
	if h.Port != "" {
		args = append(args, "-p", h.Port)
	}
	args = append(args, h.Host)

	f, err := os.OpenFile(knownHosts, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	cmd := exec.Command("ssh-keyscan", args...)
	cmd.Stdout = f
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func touchFile(p string, mode os.FileMode) error {
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE, mode)
	if err != nil {
		return err
	}
	return f.Close()
}
