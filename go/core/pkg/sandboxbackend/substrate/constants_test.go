package substrate

import (
	"strings"
	"testing"
)

func TestAcpSandboxRepositoryBase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		repo string
		want string
	}{
		{name: "default app repo", repo: "kagent-dev/kagent/app", want: "kagent-dev/kagent"},
		{name: "trailing slash", repo: "kagent-dev/kagent/app/", want: "kagent-dev/kagent"},
		{name: "leading slash", repo: "/kagent-dev/kagent/app", want: "kagent-dev/kagent"},
		{name: "two segments", repo: "myorg/app", want: "myorg"},
		{name: "single segment", repo: "kagent", want: "kagent"},
		{name: "empty", repo: "", want: ""},
		{name: "whitespace", repo: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := acpSandboxRepositoryBase(tt.repo); got != tt.want {
				t.Fatalf("acpSandboxRepositoryBase(%q) = %q, want %q", tt.repo, got, tt.want)
			}
		})
	}
}

func TestAcpSandboxImageResolve(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	t.Run("composes registry repo name digest", func(t *testing.T) {
		cfg := acpSandboxImageConfig{Registry: "cr.kagent.dev", Repository: "kagent-dev/kagent/app"}
		got, err := cfg.resolve("acp-sandbox-openclaw", digest)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		want := "cr.kagent.dev/kagent-dev/kagent/acp-sandbox-openclaw@" + digest
		if got != want {
			t.Fatalf("resolve = %q, want %q", got, want)
		}
	})

	t.Run("honors registry override for mirrored registry", func(t *testing.T) {
		cfg := acpSandboxImageConfig{Registry: "registry.internal:5000", Repository: "mirror/kagent/app"}
		got, err := cfg.resolve("acp-sandbox-hermes", digest)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		want := "registry.internal:5000/mirror/kagent/acp-sandbox-hermes@" + digest
		if got != want {
			t.Fatalf("resolve = %q, want %q", got, want)
		}
	})

	t.Run("adds sha256 prefix when missing", func(t *testing.T) {
		cfg := acpSandboxImageConfig{Registry: "cr.kagent.dev", Repository: "kagent-dev/kagent/app"}
		got, err := cfg.resolve("acp-sandbox-openclaw", strings.TrimPrefix(digest, "sha256:"))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		want := "cr.kagent.dev/kagent-dev/kagent/acp-sandbox-openclaw@" + digest
		if got != want {
			t.Fatalf("resolve = %q, want %q", got, want)
		}
	})

	t.Run("errors when digest missing", func(t *testing.T) {
		cfg := acpSandboxImageConfig{Registry: "cr.kagent.dev", Repository: "kagent-dev/kagent/app"}
		if _, err := cfg.resolve("acp-sandbox-openclaw", "  "); err == nil {
			t.Fatal("expected error for missing digest")
		}
	})

	t.Run("errors when registry missing", func(t *testing.T) {
		cfg := acpSandboxImageConfig{Repository: "kagent-dev/kagent/app"}
		if _, err := cfg.resolve("acp-sandbox-openclaw", digest); err == nil {
			t.Fatal("expected error for missing registry")
		}
	})

	t.Run("errors when repository missing", func(t *testing.T) {
		cfg := acpSandboxImageConfig{Registry: "cr.kagent.dev"}
		if _, err := cfg.resolve("acp-sandbox-openclaw", digest); err == nil {
			t.Fatal("expected error for missing repository")
		}
	})
}

func TestAcpSandboxImageHelpers(t *testing.T) {
	const digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	prevOpenClaw := AcpSandboxOpenClawImageDigest
	prevHermes := AcpSandboxHermesImageDigest
	t.Cleanup(func() {
		AcpSandboxOpenClawImageDigest = prevOpenClaw
		AcpSandboxHermesImageDigest = prevHermes
	})
	AcpSandboxOpenClawImageDigest = digest
	AcpSandboxHermesImageDigest = digest

	cfg := acpSandboxImageConfig{Registry: "localhost:5001", Repository: "kagent-dev/kagent/app"}

	openClaw, err := acpSandboxOpenClawImage(cfg)
	if err != nil {
		t.Fatalf("acpSandboxOpenClawImage: %v", err)
	}
	if want := "localhost:5001/kagent-dev/kagent/acp-sandbox-openclaw@" + digest; openClaw != want {
		t.Fatalf("acpSandboxOpenClawImage = %q, want %q", openClaw, want)
	}

	hermes, err := acpSandboxHermesImage(cfg)
	if err != nil {
		t.Fatalf("acpSandboxHermesImage: %v", err)
	}
	if want := "localhost:5001/kagent-dev/kagent/acp-sandbox-hermes@" + digest; hermes != want {
		t.Fatalf("acpSandboxHermesImage = %q, want %q", hermes, want)
	}
}
