// Package output owns the CLI's shared machine-output contract.
package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// FlagName is the root persistent flag that selects the output format.
const FlagName = "output-format"

// FromCommand reads the output format a command was invoked with.
func FromCommand(cmd *cobra.Command) (string, error) {
	return cmd.Flags().GetString(FlagName)
}

// Format selects the CLI payload encoding.
type Format string

const (
	// FormatTable emits human-readable text.
	FormatTable Format = "table"
	// FormatJSON emits protobuf JSON.
	FormatJSON Format = "json"
)

// Parse validates a CLI output format.
func Parse(value string) (Format, error) {
	format := Format(value)
	switch format {
	case FormatTable, FormatJSON:
		return format, nil
	default:
		return "", fmt.Errorf("unsupported output format %q: must be table or json", value)
	}
}

// WriteProto writes one protobuf message as a JSON line.
func WriteProto(w io.Writer, message proto.Message) error {
	data, err := protojson.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal JSON output: %w", err)
	}
	if _, err := fmt.Fprintln(w, string(data)); err != nil {
		return fmt.Errorf("write JSON output: %w", err)
	}
	return nil
}

// WriteJSON writes one JSON value as a line.
func WriteJSON(w io.Writer, value any) error {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return fmt.Errorf("write JSON output: %w", err)
	}
	return nil
}
