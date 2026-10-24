package declarative

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/ext"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/genai"
)

// CEL variables. Every scope sees the ADK context of the running agent; each
// scope adds the value it decides on.
const (
	celState      = "state"      // session state, map(string, dyn)
	celSession    = "session"    // {id, app_name, user_id}
	celUser       = "user"       // {id, content}: content is the text that started the invocation
	celInvocation = "invocation" // {id, branch, agent_name}
	celInput      = "input"      // node input (cel nodes)
	celRequest    = "request"    // {model, text, contents, system_instruction, tools} (before_model)
	celResponse   = "response"   // {text, partial, turn_complete, error, finish_reason, function_calls} (after_model)
	celTool       = "tool"       // {name, args} and, after the call, {result, error}
	celError      = "error"      // {message, timeout, canceled} (retry.when only)
)

// celScope selects the variables an expression may use.
type celScope string

const (
	scopeNode        celScope = "node"
	scopeBeforeModel celScope = "before_model"
	scopeAfterModel  celScope = "after_model"
	scopeBeforeTool  celScope = "before_tool"
	scopeAfterTool   celScope = "after_tool"
	// scopeRetry sees only the error: ADK decides on a retry without the
	// agent context.
	scopeRetry celScope = "retry"
)

// celCostLimit bounds the work of one evaluation so that an expression cannot
// stall a run.
const celCostLimit = 1_000_000

var celObject = cel.MapType(cel.StringType, cel.DynType)

var celEnvs = func() map[celScope]*cel.Env {
	extra := map[celScope]cel.EnvOption{
		scopeNode:        cel.Variable(celInput, cel.DynType),
		scopeBeforeModel: cel.Variable(celRequest, celObject),
		scopeAfterModel:  cel.Variable(celResponse, celObject),
		scopeBeforeTool:  cel.Variable(celTool, celObject),
		scopeAfterTool:   cel.Variable(celTool, celObject),
		scopeRetry:       cel.Variable(celError, celObject),
	}
	envs := make(map[celScope]*cel.Env, len(extra))
	for scope, opt := range extra {
		opts := []cel.EnvOption{opt}
		if scope != scopeRetry {
			opts = append(opts,
				cel.Variable(celState, celObject),
				cel.Variable(celSession, celObject),
				cel.Variable(celUser, celObject),
				cel.Variable(celInvocation, celObject))
		}
		env, err := cel.NewEnv(append(opts,
			// The cel-go standard extensions: cel.bind locals, strings,
			// base64, lists, math, sets, RE2 regex helpers (regex.extract
			// returns an optional, hence optional types).
			cel.OptionalTypes(), ext.Bindings(), ext.Strings(), ext.Encoders(),
			ext.Lists(), ext.Math(), ext.Sets(), ext.Regex(),
			jsonLibrary,
		)...)
		if err != nil {
			panic(fmt.Sprintf("declarative CEL environment %s: %v", scope, err))
		}
		envs[scope] = env
	}
	return envs
}()

// celResult constrains the static type of an expression.
type celResult int

const (
	resultAny celResult = iota
	resultBool
	resultRoutes
)

// expression is a compiled CEL program.
type expression struct {
	source  string
	program cel.Program
}

// compileCEL type-checks source in scope. Dynamically typed results are
// accepted and checked again when evaluated.
func compileCEL(scope celScope, source string, want celResult) (*expression, error) {
	env := celEnvs[scope]
	ast, issues := env.Compile(source)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile CEL %q: %w", source, issues.Err())
	}
	out := ast.OutputType()
	switch want {
	case resultBool:
		if !out.IsAssignableType(cel.BoolType) && !out.IsExactType(cel.DynType) {
			return nil, fmt.Errorf("CEL %q has type %s, want bool", source, out)
		}
	case resultRoutes:
		if !out.IsAssignableType(cel.StringType) && !out.IsAssignableType(cel.ListType(cel.StringType)) && !out.IsExactType(cel.DynType) {
			return nil, fmt.Errorf("CEL %q has type %s, want string or list(string)", source, out)
		}
	}
	program, err := env.Program(ast, cel.CostLimit(celCostLimit))
	if err != nil {
		return nil, fmt.Errorf("plan CEL %q: %w", source, err)
	}
	return &expression{source: source, program: program}, nil
}

func (e *expression) eval(vars map[string]any) (any, error) {
	val, _, err := e.program.Eval(vars)
	if err != nil {
		return nil, fmt.Errorf("evaluate CEL %q: %w", e.source, err)
	}
	return nativeValue(val)
}

func (e *expression) evalBool(vars map[string]any) (bool, error) {
	out, err := e.eval(vars)
	if err != nil {
		return false, err
	}
	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("CEL %q returned %T, want bool", e.source, out)
	}
	return b, nil
}

// evalRoutes evaluates a route expression. Empty strings select no route.
func (e *expression) evalRoutes(vars map[string]any) ([]string, error) {
	out, err := e.eval(vars)
	if err != nil {
		return nil, err
	}
	switch v := out.(type) {
	case nil:
		return nil, nil
	case string:
		if v == "" {
			return nil, nil
		}
		return []string{v}, nil
	case []any:
		routes := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("CEL %q returned list item %T, want string", e.source, item)
			}
			if s != "" {
				routes = append(routes, s)
			}
		}
		return routes, nil
	}
	return nil, fmt.Errorf("CEL %q returned %T, want string or list of strings", e.source, out)
}

// contextVars binds the ADK context shared by every scope.
func contextVars(ctx agent.ReadonlyContext) map[string]any {
	state := map[string]any{}
	if ctx == nil {
		return map[string]any{celState: state, celSession: map[string]any{}, celUser: map[string]any{}, celInvocation: map[string]any{}}
	}
	if rs := ctx.ReadonlyState(); rs != nil {
		maps.Insert(state, rs.All())
	}
	return map[string]any{
		celState:      plainValue(state),
		celSession:    map[string]any{"id": ctx.SessionID(), "app_name": ctx.AppName(), "user_id": ctx.UserID()},
		celUser:       map[string]any{"id": ctx.UserID(), "content": contentText(ctx.UserContent())},
		celInvocation: map[string]any{"id": ctx.InvocationID(), "branch": ctx.Branch(), "agent_name": ctx.AgentName()},
	}
}

// plainValue converts runtime values into JSON-shaped Go values so CEL sees
// maps, lists, strings, int64 and float64 whatever produced them.
func plainValue(v any) any {
	switch t := v.(type) {
	case nil, string, bool, int64, float64:
		return t
	case int:
		return int64(t)
	case *genai.Content:
		return contentText(t)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return fmt.Sprint(v)
	}
	return normalizeNumbers(out)
}

func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, _ := t.Float64()
		return f
	case map[string]any:
		for k, item := range t {
			t[k] = normalizeNumbers(item)
		}
	case []any:
		for i, item := range t {
			t[i] = normalizeNumbers(item)
		}
	}
	return v
}

func contentText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range c.Parts {
		if p != nil {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// nativeValue converts a CEL result into plain Go values.
func nativeValue(val ref.Val) (any, error) {
	if types.IsError(val) {
		return nil, fmt.Errorf("CEL error: %v", val)
	}
	switch v := val.(type) {
	case types.Null:
		return nil, nil
	case types.String:
		return string(v), nil
	case types.Bool:
		return bool(v), nil
	case types.Int:
		return int64(v), nil
	case types.Uint:
		return int64(v), nil
	case types.Double:
		return float64(v), nil
	case *types.Optional:
		// An empty optional, such as a failed regex.extract, is null.
		if !v.HasValue() {
			return nil, nil
		}
		return nativeValue(v.GetValue())
	case traits.Mapper:
		out := map[string]any{}
		it := v.Iterator()
		for it.HasNext() == types.True {
			key := it.Next()
			k, ok := key.(types.String)
			if !ok {
				return nil, fmt.Errorf("CEL map key %v is not a string", key)
			}
			item, err := nativeValue(v.Get(key))
			if err != nil {
				return nil, err
			}
			out[string(k)] = item
		}
		return out, nil
	case traits.Lister:
		out := []any{}
		it := v.Iterator()
		for it.HasNext() == types.True {
			item, err := nativeValue(it.Next())
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	}
	return val.Value(), nil
}

// jsonLibrary adds json.encode(dyn) -> string and json.decode(string) -> dyn,
// for tool results and arguments that travel as JSON text.
var jsonLibrary = cel.Lib(jsonLib{})

type jsonLib struct{}

func (jsonLib) LibraryName() string { return "kagent.declarative.json" }

func (jsonLib) ProgramOptions() []cel.ProgramOption { return nil }

func (jsonLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("json.encode", cel.Overload("json_encode_dyn", []*cel.Type{cel.DynType}, cel.StringType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				native, err := nativeValue(v)
				if err != nil {
					return types.NewErr("json.encode: %v", err)
				}
				data, err := json.Marshal(native)
				if err != nil {
					return types.NewErr("json.encode: %v", err)
				}
				return types.String(data)
			}))),
		cel.Function("json.decode", cel.Overload("json_decode_string", []*cel.Type{cel.StringType}, cel.DynType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				s, ok := v.(types.String)
				if !ok {
					return types.MaybeNoSuchOverloadErr(v)
				}
				dec := json.NewDecoder(strings.NewReader(string(s)))
				dec.UseNumber()
				var out any
				if err := dec.Decode(&out); err != nil {
					return types.NewErr("json.decode: %v", err)
				}
				if dec.More() {
					return types.NewErr("json.decode: trailing data")
				}
				return types.DefaultTypeAdapter.NativeToValue(normalizeNumbers(out))
			}))),
	}
}
