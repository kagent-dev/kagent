package common

import (
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestParseRefString(t *testing.T) {
	tests := []struct {
		name            string
		ref             string
		parentNamespace string
		want            types.NamespacedName
		wantErr         bool
		errContains     string
	}{
		// Happy paths
		{
			name: "Name only",
			ref:"test-name",
			parentNamespace: "default",
			want: types.NamespacedName{
				Namespace: "default",
				Name:"test-name",
			},
			wantErr: false,
		},
		{
			name: "Full reference",
			ref: "test-namespace/test-name",
			parentNamespace: "default",
			want: types.NamespacedName{
				Namespace: "test-namespace",
				Name:"test-name",
			},
			wantErr: false,
		},
		{
			name: "Full reference with empty parent namespace",
			ref: "test-namespace/test-name",
			parentNamespace: "",
			want: types.NamespacedName{
				Namespace: "test-namespace",
				Name:"test-name",
			},
			wantErr: false,
		},


		// Error cases
		{
			name: "Empty reference",
			ref: "",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "empty reference string",
		},
		{
			name: "Empty parent namespace with name only",
			ref: "test-name",
			parentNamespace: "",
			want: types.NamespacedName{},
			wantErr: true,
			errContains:"parent namespace cannot be empty when reference doesn't contain namespace",
		},
		{
			name: "Too many slashes",
			ref: "namespace/name/extra",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains:"reference cannot contain more than one slash",
		},
		{
			name: "Empty name in full reference",
			ref: "namespace/",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains:"name cannot be empty",
		},
		{
			name: "Empty namespace in full reference",
			ref: "/name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "namespace cannot be empty",
		},
		{
			name: "Empty name and namespace in full reference",
			ref: "/",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains:"namespace and name cannot be empty",
		},
		{
			name: "Only spaces as name",
			ref: "    ",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains:"name cannot contain whitespace characters",
		},
		{
			name: "Only spaces in full reference",
			ref: "  /  ",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains:"namespace cannot contain whitespace characters",
		},

		// Error cases - RFC 1123 validation
		{
			name: "Namespace with uppercase letters",
			ref: "TESt-Namespace/test-name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid namespace",
		},
		{
			name: "Namespace with special characters",
			ref: "test@namespace/test-name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid namespace",
		},
		{
			name: "Namespace starting with hyphen",
			ref: "-test-namespace/test-name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid namespace",
		},
		{
			name: "Namespace ending with hyphen",
			ref: "test-namespace-/test-name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid namespace",
		},
		{
			name: "Very long namespace name",
			ref: strings.Repeat("a", 254) + "/test-name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid namespace",
		},
		{
			name: "Namespace with leading space",
			ref: " test-namespace/test-name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "namespace cannot contain whitespace characters",
		},
		{
			name: "Name with uppercase letters",
			ref: "test-namespace/TESt-Name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid name",
		},
		{
			name: "Name with special characters",
			ref: "test-namespace/test@test-name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid name",
		},
		{
			name: "Name starting with hyphen",
			ref: "test-namespace/-test-name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid name",
		},
		{
			name: "Name ending with hyphen",
			ref: "test-namespace/test-name-",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid name",
		},
		{
			name: "Very long name",
			ref: "test-name/" + strings.Repeat("a", 254),
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "invalid name",
		},
		{
			name: "Name with leading space",
			ref: "test-namespace/ test-name",
			parentNamespace: "default",
			want: types.NamespacedName{},
			wantErr: true,
			errContains: "name cannot contain whitespace characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRefString(tt.ref, tt.parentNamespace)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRefString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// If expecting error, check if error message contains expected string
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("ParseRefString() error = %v, wantErr %v", err.Error(), tt.errContains)
				return
			}

			// Check result
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseRefString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateDNS1123Subdomain(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		fieldName   string
		wantErr     bool
		errContains string
	} {
		{
			name: "Valid label",
			value: "test-name",
			fieldName: "name",
			wantErr: false,
		},
		{
			name:        "Empty value",
			value:       "",
			fieldName:   "name",
			wantErr:     true,
			errContains: "name cannot be empty",
		},
		{
			name: "Invalid characters",
			value: "test@name",
			fieldName:   "name",
			wantErr:     true,
			errContains: "invalid name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDNS1123Subdomain(tt.value, tt.fieldName)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateDNS1123Subdomain() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("validateDNS1123Subdomain() error = %v, expected to contain %v", err.Error(), tt.errContains)
			}
		})
	}
}
