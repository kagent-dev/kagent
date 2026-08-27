package substrate

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// A Secret built in memory has StringData set and Data empty, because StringData is
// write-only and the API server folds it into Data on create. The sandbox path passes
// exactly such a Secret, so reading only Data found nothing and every sandbox agent
// failed to reconcile with "does not contain key config.json" about content that was
// present. This pins both halves.
func TestSecretValue(t *testing.T) {
	tests := []struct {
		name   string
		secret *corev1.Secret
		key    string
		want   string
		wantOK bool
	}{
		{
			name:   "a Secret read from the cluster has Data",
			secret: &corev1.Secret{Data: map[string][]byte{"config.json": []byte("{\"from\":\"data\"}")}},
			key:    "config.json",
			want:   "{\"from\":\"data\"}",
			wantOK: true,
		},
		{
			name:   "a Secret built in memory has only StringData",
			secret: &corev1.Secret{StringData: map[string]string{"config.json": "{\"from\":\"stringdata\"}"}},
			key:    "config.json",
			want:   "{\"from\":\"stringdata\"}",
			wantOK: true,
		},
		{
			// Data is what the API server produced, so it wins where both are set.
			name: "Data wins when both carry the key",
			secret: &corev1.Secret{
				Data:       map[string][]byte{"config.json": []byte("data")},
				StringData: map[string]string{"config.json": "stringdata"},
			},
			key:    "config.json",
			want:   "data",
			wantOK: true,
		},
		{
			name:   "a key in neither is still absent",
			secret: &corev1.Secret{StringData: map[string]string{"other": "x"}},
			key:    "config.json",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := secretValue(tt.secret, tt.key)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && string(got) != tt.want {
				t.Errorf("value = %q, want %q", string(got), tt.want)
			}
		})
	}
}
