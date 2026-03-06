package compaction

import "testing"

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "disabled_config_is_valid",
			config: Config{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid_enabled_config",
			config: Config{
				Enabled:            true,
				CompactionInterval: 5,
				OverlapSize:        2,
			},
			wantErr: false,
		},
		{
			name: "zero_compaction_interval",
			config: Config{
				Enabled:            true,
				CompactionInterval: 0,
				OverlapSize:        2,
			},
			wantErr: true,
		},
		{
			name: "negative_overlap_size",
			config: Config{
				Enabled:            true,
				CompactionInterval: 5,
				OverlapSize:        -1,
			},
			wantErr: true,
		},
		{
			name: "overlap_equals_interval",
			config: Config{
				Enabled:            true,
				CompactionInterval: 5,
				OverlapSize:        5,
			},
			wantErr: true,
		},
		{
			name: "overlap_exceeds_interval",
			config: Config{
				Enabled:            true,
				CompactionInterval: 5,
				OverlapSize:        6,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("DefaultConfig().Enabled = true, want false")
	}

	if cfg.CompactionInterval != 5 {
		t.Errorf("DefaultConfig().CompactionInterval = %d, want 5", cfg.CompactionInterval)
	}

	if cfg.OverlapSize != 2 {
		t.Errorf("DefaultConfig().OverlapSize = %d, want 2", cfg.OverlapSize)
	}

	// Default config should be valid
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig().Validate() failed: %v", err)
	}
}
