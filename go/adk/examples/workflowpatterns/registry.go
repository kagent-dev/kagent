package main

import (
	"errors"
	"fmt"

	"google.golang.org/adk/v2/agent"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
)

// textFunc registers a text function for function nodes.
func textFunc(f func(string) string) declarative.Func {
	return func(_ agent.Context, in any) (declarative.Result, error) {
		return declarative.Output(f(fmt.Sprint(in))), nil
	}
}

// routeFunc registers a routing decision: the input is forwarded unchanged
// and the chosen routes select the outgoing edges.
func routeFunc(f func(string) []string) declarative.Func {
	return func(_ agent.Context, in any) (declarative.Result, error) {
		return declarative.Route(in, f(fmt.Sprint(in))...), nil
	}
}

// newRegistry returns a fresh registry holding every behavior the YAML
// patterns name. Each pattern run gets its own, so stateful functions such as
// the flaky step start from zero.
func newRegistry() (*declarative.Registry, error) {
	reg := declarative.NewRegistry()
	if err := reg.RegisterModel("history", historyModel{}); err != nil {
		return nil, err
	}
	if err := reg.RegisterModel("caller", callerModel{tool: "specialist"}); err != nil {
		return nil, err
	}

	functions := map[string]declarative.Func{
		"clean": textFunc(clean), "upper": textFunc(upper), "lower": textFunc(lower),
		"exclaim": textFunc(exclaim), "reverse": textFunc(reverse), "truncate": textFunc(truncate),
		"critic": textFunc(critic), "plan": textFunc(plan), "execute": textFunc(execute),
		"bug_handler": textFunc(bugHandler), "billing_handler": textFunc(billingHandler),
		"fallback_reply": textFunc(fallbackReply), "auditor": textFunc(auditor),
		"notifier": textFunc(notifier), "publisher": textFunc(publisher),
		"rejecter": textFunc(rejecter), "final_report": textFunc(finalReport),
		"draft_text": textFunc(draftText),

		"classify": routeFunc(classify), "is_long": routeFunc(isLong), "audience": routeFunc(audience),
		"long_enough": routeFunc(longEnough), "verdict": routeFunc(verdict),

		"split": func(_ agent.Context, in any) (declarative.Result, error) {
			return declarative.Output(words(fmt.Sprint(in))), nil
		},
		"join_words": func(_ agent.Context, in any) (declarative.Result, error) {
			items, _ := in.([]any)
			return declarative.Output(joinWords(items)), nil
		},
		"combine": func(_ agent.Context, in any) (declarative.Result, error) {
			branches, _ := in.(map[string]any)
			return declarative.Output(combine(branches)), nil
		},
		"make_person": func(_ agent.Context, in any) (declarative.Result, error) {
			return declarative.Output(makePerson(fmt.Sprint(in))), nil
		},
		"greet_person": func(_ agent.Context, in any) (declarative.Result, error) {
			person, _ := in.(map[string]any)
			return declarative.Output(greetPerson(person)), nil
		},
		"customer_args": func(_ agent.Context, in any) (declarative.Result, error) {
			return declarative.Output(customerArgs(fmt.Sprint(in))), nil
		},
		"describe_customer": func(_ agent.Context, in any) (declarative.Result, error) {
			customer, _ := in.(map[string]any)
			return declarative.Output(describeCustomer(customer)), nil
		},
	}
	flaky := newFlaky(2)
	functions["flaky"] = func(_ agent.Context, in any) (declarative.Result, error) {
		out, err := flaky(fmt.Sprint(in))
		return declarative.Output(out), err
	}
	for name, fn := range functions {
		if err := reg.RegisterFunction(name, fn); err != nil {
			return nil, err
		}
	}

	// A dynamic node that runs two reviewers in turn and reports both answers.
	if err := reg.RegisterDynamic("review_both", func(_ agent.Context, in any, env *declarative.DynamicEnv) (any, error) {
		first, err := env.RunChild("ask_first", in)
		if err != nil {
			return nil, err
		}
		second, err := env.RunChild("ask_second", in)
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("first %v, second %v", first, second), nil
	}); err != nil {
		return nil, err
	}

	// A dynamic node decides at run time how often to run its child.
	if err := reg.RegisterDynamic("refine", func(_ agent.Context, in any, env *declarative.DynamicEnv) (any, error) {
		out := in
		for {
			next, err := env.RunChild("critic", out)
			if errors.Is(err, declarative.ErrIterationBudget) {
				return out, nil
			}
			if err != nil {
				return nil, err
			}
			out = next
		}
	}); err != nil {
		return nil, err
	}

	lookup, err := newLookupTool()
	if err != nil {
		return nil, err
	}
	if err := reg.RegisterTool(lookup); err != nil {
		return nil, err
	}
	return reg, nil
}
