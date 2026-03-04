package agent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitSkillName(tt.ref)
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
		{name: "absolute path rejected", path: "/etc/passwd", wantErr: "must be relative"},
		{name: "dotdot at start rejected", path: "../escape", wantErr: "must not contain '..'"},
		{name: "dotdot in middle rejected", path: "a/../b", wantErr: "must not contain '..'"},
		{name: "dotdot at end rejected", path: "a/b/..", wantErr: "must not contain '..'"},
		{name: "bare dotdot rejected", path: "..", wantErr: "must not contain '..'"},
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

func Test_prepareSkillsInitData_duplicateNames(t *testing.T) {
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
			_, err := prepareSkillsInitData(tt.gitRefs, nil, tt.ociRefs, false)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_prepareSkillsInitData_pathTraversal(t *testing.T) {
	_, err := prepareSkillsInitData(
		[]v1alpha2.GitRepo{
			{URL: "https://github.com/org/repo", Ref: "main", Path: "../escape"},
		},
		nil, nil, false,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not contain '..'")
}

func Test_prepareSkillsInitData_absolutePath(t *testing.T) {
	_, err := prepareSkillsInitData(
		[]v1alpha2.GitRepo{
			{URL: "https://github.com/org/repo", Ref: "main", Path: "/etc/passwd"},
		},
		nil, nil, false,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be relative")
}

func Test_prepareSkillsInitData_authMountPath(t *testing.T) {
	data, err := prepareSkillsInitData(
		[]v1alpha2.GitRepo{{URL: "https://github.com/org/repo", Ref: "main"}},
		&corev1.LocalObjectReference{Name: "my-secret"},
		nil, false,
	)
	require.NoError(t, err)
	assert.Equal(t, "/git-auth", data.AuthMountPath)
}
