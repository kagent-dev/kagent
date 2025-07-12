package cli

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/spf13/viper"
)

type OutputFormat string

const (
	OutputFormatJSON  OutputFormat = "json"
	OutputFormatTable OutputFormat = "table"
)

func printOutput(data interface{}, tableHeaders []string, tableRows [][]string) error {
	format := OutputFormat(viper.GetString("output_format"))

	tw := table.NewWriter()
	headers := slices.Collect(utils.Map(slices.Values(tableHeaders), func(header string) interface{} {
		return header
	}))
	tw.AppendHeader(headers)
	rows := slices.Collect(utils.Map(slices.Values(tableRows), func(row []string) table.Row {
		return slices.Collect(utils.Map(slices.Values(row), func(cell string) interface{} {
			return cell
		}))
	}))
	tw.AppendRows(rows)

	switch format {
	case OutputFormatJSON:
		return printJSON(data)
	case OutputFormatTable:
		fmt.Println(tw.Render())
		return nil
	default:
		return fmt.Errorf("unknown output format: %s", format)
	}
}

func printJSON(data interface{}) error {
	output, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("error formatting JSON: %w", err)
	}
	fmt.Println(string(output))
	return nil
}
