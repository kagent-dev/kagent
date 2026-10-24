// Command workflowpatterns runs each workflow design pattern twice, once built
// with the ADK Go API and once from its declarative YAML document, and prints
// what each produced. See docs/architecture/workflow-patterns.md.
//
//	go run ./adk/examples/workflowpatterns -list
//	go run ./adk/examples/workflowpatterns -pattern router -input "bug: crash"
//	go run ./adk/examples/workflowpatterns -pattern human_approval -input "ship it" -reply yes
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"google.golang.org/adk/v2/agent"
)

type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(s string) error { *r = append(*r, s); return nil }

func main() {
	list := flag.Bool("list", false, "list the patterns")
	name := flag.String("pattern", "", "pattern to run")
	input := flag.String("input", "", "user message")
	mode := flag.String("mode", "both", "go, yaml or both")
	var replies repeated
	flag.Var(&replies, "reply", "answer to a human-input request (repeatable)")
	flag.Parse()

	if err := run(context.Background(), *list, *name, *input, *mode, replies); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, list bool, name, input, mode string, replies []string) error {
	if list {
		for _, p := range Catalog() {
			fmt.Printf("%-18s %s\n", p.Name, p.Title)
		}
		return nil
	}
	p, ok := Find(name)
	if !ok {
		return fmt.Errorf("unknown pattern %q; use -list", name)
	}
	if !slices.Contains([]string{"go", "yaml", "both"}, mode) {
		return fmt.Errorf("mode must be go, yaml or both, got %q", mode)
	}
	builders := []struct {
		label string
		build func() (agent.Agent, error)
	}{
		{"go", p.Go},
		{"yaml", func() (agent.Agent, error) { return p.BuildYAML(ctx) }},
	}
	traces := map[string][]string{}
	for _, b := range builders {
		if mode != "both" && mode != b.label {
			continue
		}
		root, err := b.build()
		if err != nil {
			return fmt.Errorf("failed to build %s version: %w", b.label, err)
		}
		trace, err := Run(ctx, root, input, replies)
		if err != nil {
			return fmt.Errorf("failed to run %s version: %w", b.label, err)
		}
		traces[b.label] = p.Normalize(trace)
		fmt.Printf("== %s ==\n", b.label)
		for _, line := range traces[b.label] {
			fmt.Println(line)
		}
	}
	if mode == "both" {
		if slices.Equal(traces["go"], traces["yaml"]) {
			fmt.Println("== identical ==")
		} else {
			return fmt.Errorf("the Go and YAML versions diverged")
		}
	}
	return nil
}
