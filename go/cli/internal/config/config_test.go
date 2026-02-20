package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestGetReturnsViperValues(t *testing.T) {
	customURL := "http://custom-kagent:9090"
	customNamespace := "my-namespace"
	customFormat := "json"
	customTimeout := 60 * time.Second

	t.Run("timeout from time.Duration", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)

		viper.Set("kagent_url", customURL)
		viper.Set("namespace", customNamespace)
		viper.Set("output_format", customFormat)
		viper.Set("verbose", true)
		viper.Set("timeout", customTimeout)

		cfg, err := Get()
		if err != nil {
			t.Fatalf("expected no error from Get(), got %v", err)
		}
		if cfg.KAgentURL != customURL {
			t.Errorf("KAgentURL = %q, want %q", cfg.KAgentURL, customURL)
		}
		if cfg.Namespace != customNamespace {
			t.Errorf("Namespace = %q, want %q", cfg.Namespace, customNamespace)
		}
		if cfg.OutputFormat != customFormat {
			t.Errorf("OutputFormat = %q, want %q", cfg.OutputFormat, customFormat)
		}
		if !cfg.Verbose {
			t.Error("Verbose = false, want true")
		}
		if cfg.Timeout != customTimeout {
			t.Errorf("Timeout = %v, want %v", cfg.Timeout, customTimeout)
		}
	})

	t.Run("timeout from string", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)

		viper.Set("kagent_url", customURL)
		viper.Set("namespace", customNamespace)
		viper.Set("output_format", customFormat)
		viper.Set("verbose", true)
		viper.Set("timeout", "60s")

		cfg, err := Get()
		if err != nil {
			t.Fatalf("expected no error from Get() with string timeout, got %v", err)
		}
		if cfg.Timeout != customTimeout {
			t.Errorf("Timeout = %v, want %v", cfg.Timeout, customTimeout)
		}
	})

	t.Run("timeout from compound string", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)

		viper.Set("timeout", "5m0s")

		cfg, err := Get()
		if err != nil {
			t.Fatalf("expected no error from Get() with compound string timeout, got %v", err)
		}
		want := 5 * time.Minute
		if cfg.Timeout != want {
			t.Errorf("Timeout = %v, want %v", cfg.Timeout, want)
		}
	})
}

func TestGetReturnsDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	// Set up viper defaults the same way Init() does, so this test verifies
	// the actual default values a user gets rather than bare zero-values.
	viper.SetDefault("kagent_url", "http://localhost:8083")
	viper.SetDefault("output_format", "table")
	viper.SetDefault("namespace", "kagent")
	viper.SetDefault("timeout", 300*time.Second)

	cfg, err := Get()
	if err != nil {
		t.Fatalf("expected no error from Get(), got %v", err)
	}
	if cfg.KAgentURL != "http://localhost:8083" {
		t.Errorf("KAgentURL = %q, want %q", cfg.KAgentURL, "http://localhost:8083")
	}
	if cfg.Namespace != "kagent" {
		t.Errorf("Namespace = %q, want %q", cfg.Namespace, "kagent")
	}
	if cfg.OutputFormat != "table" {
		t.Errorf("OutputFormat = %q, want %q", cfg.OutputFormat, "table")
	}
	if cfg.Verbose {
		t.Error("Verbose = true, want false")
	}
	if cfg.Timeout != 300*time.Second {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, 300*time.Second)
	}
}

func TestGetReturnsZeroValuesWhenViperEmpty(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg, err := Get()
	if err != nil {
		t.Fatalf("expected no error from Get(), got %v", err)
	}
	// With no viper values or defaults set, all fields should be zero values.
	if cfg.KAgentURL != "" {
		t.Errorf("KAgentURL = %q, want empty", cfg.KAgentURL)
	}
	if cfg.Namespace != "" {
		t.Errorf("Namespace = %q, want empty", cfg.Namespace)
	}
	if cfg.OutputFormat != "" {
		t.Errorf("OutputFormat = %q, want empty", cfg.OutputFormat)
	}
	if cfg.Verbose {
		t.Error("Verbose = true, want false")
	}
	if cfg.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0", cfg.Timeout)
	}
}
