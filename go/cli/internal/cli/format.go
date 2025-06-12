package cli

import (
	"encoding/json"
	"fmt"
	"iter"
	"slices"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/viper"
)

type OutputFormat string

const (
	OutputFormatJSON  OutputFormat = "json"
	OutputFormatTable OutputFormat = "table"
)

// Map returns an iterator over the slice, applying the function f to each element.
func Map[E any, F any](s iter.Seq[E], f func(E) F) iter.Seq[F] {
	return func(yield func(F) bool) {
		next, stop := iter.Pull(s)
		defer stop()
		for {
			v, ok := next()
			if !ok {
				break
			}
			if !yield(f(v)) {
				break
			}
		}
	}
}

func Filter[E any](s iter.Seq[E], f func(E) bool) iter.Seq[E] {
	return func(yield func(E) bool) {
		next, stop := iter.Pull(s)
		defer stop()
		for {
			v, ok := next()
			if !ok {
				break
			}
			if f(v) && !yield(v) {
				break
			}
		}
	}
}

func printOutput(data interface{}, tableHeaders []string, tableRows [][]string) error {
	format := OutputFormat(viper.GetString("output_format"))

	tw := table.NewWriter()
	headers := slices.Collect(Map(slices.Values(tableHeaders), func(header string) interface{} {
		return header
	}))
	tw.AppendHeader(headers)
	rows := slices.Collect(Map(slices.Values(tableRows), func(row []string) table.Row {
		return slices.Collect(Map(slices.Values(row), func(cell string) interface{} {
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
