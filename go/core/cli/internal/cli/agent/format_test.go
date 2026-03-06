package cli

import (
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/database"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPrintJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    any
		wantErr bool
	}{
		{
			name:    "simple map",
			data:    map[string]string{"key": "value"},
			wantErr: false,
		},
		{
			name:    "slice",
			data:    []string{"a", "b", "c"},
			wantErr: false,
		},
		{
			name:    "struct",
			data:    struct{ Name string }{"test"},
			wantErr: false,
		},
		{
			name:    "nil",
			data:    nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := printJSON(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPrintOutput_TableFormat(t *testing.T) {
	// Set output format to table
	viper.Set("output_format", "table")
	defer viper.Reset()

	data := []map[string]string{{"key": "value"}}
	headers := []string{"HEADER1", "HEADER2"}
	rows := [][]string{{"row1col1", "row1col2"}}

	err := printOutput(data, headers, rows)
	assert.NoError(t, err)
}

func TestPrintOutput_JSONFormat(t *testing.T) {
	// Set output format to json
	viper.Set("output_format", "json")
	defer viper.Reset()

	data := []map[string]string{{"key": "value"}}
	headers := []string{"HEADER1", "HEADER2"}
	rows := [][]string{{"row1col1", "row1col2"}}

	err := printOutput(data, headers, rows)
	assert.NoError(t, err)
}

func TestPrintOutput_UnknownFormat(t *testing.T) {
	// Set output format to unknown value
	viper.Set("output_format", "unknown")
	defer viper.Reset()

	data := []map[string]string{{"key": "value"}}
	headers := []string{"HEADER1", "HEADER2"}
	rows := [][]string{{"row1col1", "row1col2"}}

	err := printOutput(data, headers, rows)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestPrintTools(t *testing.T) {
	now := time.Now()
	tools := []database.Tool{
		{
			ID:          "tool1",
			ServerName:  "server1",
			Description: "Test tool 1",
			CreatedAt:   now,
		},
		{
			ID:          "tool2",
			ServerName:  "server2",
			Description: "Test tool 2",
			CreatedAt:   now,
		},
	}

	viper.Set("output_format", "table")
	defer viper.Reset()

	err := printTools(tools)
	assert.NoError(t, err)
}

func TestPrintAgents(t *testing.T) {
	now := metav1.Now()
	agents := []api.AgentResponse{
		{
			Agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "agent1",
					Namespace:         "default",
					CreationTimestamp: now,
				},
			},
			DeploymentReady: true,
			Accepted:        true,
		},
		{
			Agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "agent2",
					Namespace:         "default",
					CreationTimestamp: now,
				},
			},
			DeploymentReady: false,
			Accepted:        false,
		},
	}

	viper.Set("output_format", "table")
	defer viper.Reset()

	err := printAgents(agents)
	assert.NoError(t, err)
}

func TestPrintSessions(t *testing.T) {
	now := time.Now()
	name1 := "session1"
	agentID1 := "agent1"

	sessions := []*database.Session{
		{
			ID:        "sess1",
			Name:      &name1,
			AgentID:   &agentID1,
			CreatedAt: now,
		},
		{
			ID:        "sess2",
			Name:      nil, // No name
			AgentID:   nil, // No agent
			CreatedAt: now,
		},
	}

	viper.Set("output_format", "table")
	defer viper.Reset()

	err := printSessions(sessions)
	require.NoError(t, err)
}
