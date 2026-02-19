package env

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRegisterStringVar(t *testing.T) {
	allVars = make(map[string]Var)

	tests := []struct {
		name         string
		envName      string
		defaultValue string
		envValue     string
		setEnv       bool
		want         string
	}{
		{name: "returns default when unset", envName: "TEST_STRING_DEFAULT", defaultValue: "mydefault", setEnv: false, want: "mydefault"},
		{name: "returns env value when set", envName: "TEST_STRING_SET", defaultValue: "mydefault", envValue: "override", setEnv: true, want: "override"},
		{name: "returns empty string when set empty", envName: "TEST_STRING_EMPTY", defaultValue: "mydefault", envValue: "", setEnv: true, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.envName, tt.envValue)
			}
			sv := RegisterStringVar(tt.envName, tt.defaultValue, "test desc", ComponentTesting)
			got := sv.Get()
			if got != tt.want {
				t.Errorf("StringVar.Get() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegisterBoolVar(t *testing.T) {
	allVars = make(map[string]Var)

	tests := []struct {
		name         string
		envName      string
		defaultValue bool
		envValue     string
		setEnv       bool
		want         bool
	}{
		{name: "returns default when unset", envName: "TEST_BOOL_DEFAULT", defaultValue: false, setEnv: false, want: false},
		{name: "returns true when set", envName: "TEST_BOOL_TRUE", defaultValue: false, envValue: "true", setEnv: true, want: true},
		{name: "returns false when set", envName: "TEST_BOOL_FALSE", defaultValue: true, envValue: "false", setEnv: true, want: false},
		{name: "returns default on invalid", envName: "TEST_BOOL_INVALID", defaultValue: true, envValue: "notabool", setEnv: true, want: true},
		{name: "accepts 1 as true", envName: "TEST_BOOL_ONE", defaultValue: false, envValue: "1", setEnv: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.envName, tt.envValue)
			}
			bv := RegisterBoolVar(tt.envName, tt.defaultValue, "test desc", ComponentTesting)
			got := bv.Get()
			if got != tt.want {
				t.Errorf("BoolVar.Get() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegisterIntVar(t *testing.T) {
	allVars = make(map[string]Var)

	tests := []struct {
		name         string
		envName      string
		defaultValue int
		envValue     string
		setEnv       bool
		want         int
	}{
		{name: "returns default when unset", envName: "TEST_INT_DEFAULT", defaultValue: 42, setEnv: false, want: 42},
		{name: "returns env value when set", envName: "TEST_INT_SET", defaultValue: 42, envValue: "99", setEnv: true, want: 99},
		{name: "returns default on invalid", envName: "TEST_INT_INVALID", defaultValue: 42, envValue: "notanint", setEnv: true, want: 42},
		{name: "handles zero", envName: "TEST_INT_ZERO", defaultValue: 42, envValue: "0", setEnv: true, want: 0},
		{name: "handles negative", envName: "TEST_INT_NEG", defaultValue: 0, envValue: "-5", setEnv: true, want: -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.envName, tt.envValue)
			}
			iv := RegisterIntVar(tt.envName, tt.defaultValue, "test desc", ComponentTesting)
			got := iv.Get()
			if got != tt.want {
				t.Errorf("IntVar.Get() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegisterDurationVar(t *testing.T) {
	allVars = make(map[string]Var)

	tests := []struct {
		name         string
		envName      string
		defaultValue time.Duration
		envValue     string
		setEnv       bool
		want         time.Duration
	}{
		{name: "returns default when unset", envName: "TEST_DUR_DEFAULT", defaultValue: 5 * time.Second, setEnv: false, want: 5 * time.Second},
		{name: "returns env value when set", envName: "TEST_DUR_SET", defaultValue: 5 * time.Second, envValue: "30s", setEnv: true, want: 30 * time.Second},
		{name: "returns default on invalid", envName: "TEST_DUR_INVALID", defaultValue: 5 * time.Second, envValue: "notaduration", setEnv: true, want: 5 * time.Second},
		{name: "handles minutes", envName: "TEST_DUR_MINS", defaultValue: 0, envValue: "2m", setEnv: true, want: 2 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv(tt.envName, tt.envValue)
			}
			dv := RegisterDurationVar(tt.envName, tt.defaultValue, "test desc", ComponentTesting)
			got := dv.Get()
			if got != tt.want {
				t.Errorf("DurationVar.Get() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLookup(t *testing.T) {
	allVars = make(map[string]Var)

	t.Run("StringVar Lookup unset", func(t *testing.T) {
		sv := RegisterStringVar("TEST_LOOKUP_UNSET", "default", "desc", ComponentTesting)
		val, ok := sv.Lookup()
		if ok {
			t.Error("expected ok=false for unset var")
		}
		if val != "default" {
			t.Errorf("expected default value, got %q", val)
		}
	})

	t.Run("StringVar Lookup set", func(t *testing.T) {
		t.Setenv("TEST_LOOKUP_SET", "hello")
		sv := RegisterStringVar("TEST_LOOKUP_SET", "default", "desc", ComponentTesting)
		val, ok := sv.Lookup()
		if !ok {
			t.Error("expected ok=true for set var")
		}
		if val != "hello" {
			t.Errorf("expected 'hello', got %q", val)
		}
	})

	t.Run("BoolVar Lookup unset", func(t *testing.T) {
		bv := RegisterBoolVar("TEST_BOOL_LOOKUP_UNSET", true, "desc", ComponentTesting)
		val, ok := bv.Lookup()
		if ok {
			t.Error("expected ok=false for unset var")
		}
		if val != true {
			t.Errorf("expected default true, got %v", val)
		}
	})

	t.Run("IntVar Lookup unset", func(t *testing.T) {
		iv := RegisterIntVar("TEST_INT_LOOKUP_UNSET", 10, "desc", ComponentTesting)
		val, ok := iv.Lookup()
		if ok {
			t.Error("expected ok=false for unset var")
		}
		if val != 10 {
			t.Errorf("expected default 10, got %v", val)
		}
	})

	t.Run("DurationVar Lookup unset", func(t *testing.T) {
		dv := RegisterDurationVar("TEST_DUR_LOOKUP_UNSET", 5*time.Second, "desc", ComponentTesting)
		val, ok := dv.Lookup()
		if ok {
			t.Error("expected ok=false for unset var")
		}
		if val != 5*time.Second {
			t.Errorf("expected default 5s, got %v", val)
		}
	})
}

func TestVarDescriptions(t *testing.T) {
	allVars = make(map[string]Var)

	RegisterStringVar("ZZZ_VAR", "", "last", ComponentTesting)
	RegisterStringVar("AAA_VAR", "", "first", ComponentTesting)
	RegisterBoolVar("MMM_VAR", false, "middle", ComponentTesting)

	vars := VarDescriptions()
	if len(vars) != 3 {
		t.Fatalf("expected 3 vars, got %d", len(vars))
	}

	// Verify sorted by name
	if vars[0].Name != "AAA_VAR" {
		t.Errorf("expected first var to be AAA_VAR, got %s", vars[0].Name)
	}
	if vars[1].Name != "MMM_VAR" {
		t.Errorf("expected second var to be MMM_VAR, got %s", vars[1].Name)
	}
	if vars[2].Name != "ZZZ_VAR" {
		t.Errorf("expected third var to be ZZZ_VAR, got %s", vars[2].Name)
	}
}

func TestVarByName(t *testing.T) {
	allVars = make(map[string]Var)

	RegisterStringVar("FINDME", "val", "find me", ComponentController)

	v, ok := VarByName("FINDME")
	if !ok {
		t.Fatal("expected to find FINDME")
	}
	if v.Description != "find me" {
		t.Errorf("wrong description: %s", v.Description)
	}

	_, ok = VarByName("NONEXISTENT")
	if ok {
		t.Error("expected not to find NONEXISTENT")
	}
}

func TestVarTypeString(t *testing.T) {
	tests := []struct {
		vt   VarType
		want string
	}{
		{TypeString, "String"},
		{TypeBool, "Boolean"},
		{TypeInt, "Integer"},
		{TypeFloat, "Floating-Point"},
		{TypeDuration, "Duration"},
		{VarType(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.vt.String(); got != tt.want {
				t.Errorf("VarType.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExportMarkdown(t *testing.T) {
	allVars = make(map[string]Var)

	RegisterStringVar("TEST_MD_VAR", "default", "A test variable.", ComponentController)
	RegisterBoolVar("TEST_MD_BOOL", true, "A bool variable.", ComponentController)

	md := ExportMarkdown("all")
	if !strings.Contains(md, "TEST_MD_VAR") {
		t.Error("markdown should contain TEST_MD_VAR")
	}
	if !strings.Contains(md, "A test variable.") {
		t.Error("markdown should contain description")
	}
	if !strings.Contains(md, "controller") {
		t.Error("markdown should contain component heading")
	}
}

func TestExportMarkdownFilter(t *testing.T) {
	allVars = make(map[string]Var)

	RegisterStringVar("CTRL_VAR", "", "controller var", ComponentController)
	RegisterStringVar("CLI_VAR", "", "cli var", ComponentCLI)

	md := ExportMarkdown("controller")
	if !strings.Contains(md, "CTRL_VAR") {
		t.Error("should contain controller var")
	}
	if strings.Contains(md, "CLI_VAR") {
		t.Error("should not contain cli var when filtering by controller")
	}
}

func TestExportJSON(t *testing.T) {
	allVars = make(map[string]Var)

	RegisterStringVar("JSON_VAR", "val", "json test", ComponentCLI)

	j := ExportJSON("all")
	if !strings.Contains(j, `"name": "JSON_VAR"`) {
		t.Error("JSON should contain var name")
	}
	if !strings.Contains(j, `"description": "json test"`) {
		t.Error("JSON should contain description")
	}
}

func TestHiddenVarsExcluded(t *testing.T) {
	allVars = make(map[string]Var)

	sv := RegisterStringVar("VISIBLE_VAR", "", "visible", ComponentController)
	_ = sv
	// Manually register a hidden var
	register(Var{
		Name:        "HIDDEN_VAR",
		Description: "hidden",
		Component:   ComponentController,
		Hidden:      true,
	})

	md := ExportMarkdown("all")
	if !strings.Contains(md, "VISIBLE_VAR") {
		t.Error("should contain visible var")
	}
	if strings.Contains(md, "HIDDEN_VAR") {
		t.Error("should not contain hidden var")
	}
}

func TestNameMethod(t *testing.T) {
	allVars = make(map[string]Var)

	sv := RegisterStringVar("NAME_TEST_S", "", "", ComponentTesting)
	bv := RegisterBoolVar("NAME_TEST_B", false, "", ComponentTesting)
	iv := RegisterIntVar("NAME_TEST_I", 0, "", ComponentTesting)
	dv := RegisterDurationVar("NAME_TEST_D", 0, "", ComponentTesting)

	if sv.Name() != "NAME_TEST_S" {
		t.Errorf("StringVar.Name() = %q", sv.Name())
	}
	if bv.Name() != "NAME_TEST_B" {
		t.Errorf("BoolVar.Name() = %q", bv.Name())
	}
	if iv.Name() != "NAME_TEST_I" {
		t.Errorf("IntVar.Name() = %q", iv.Name())
	}
	if dv.Name() != "NAME_TEST_D" {
		t.Errorf("DurationVar.Name() = %q", dv.Name())
	}
}

func TestDefaultValueMethod(t *testing.T) {
	allVars = make(map[string]Var)

	sv := RegisterStringVar("DEFAULT_TEST", "mydefault", "test", ComponentTesting)
	if sv.DefaultValue() != "mydefault" {
		t.Errorf("DefaultValue() = %q, want 'mydefault'", sv.DefaultValue())
	}
}

func TestStringVarGetReadsLive(t *testing.T) {
	allVars = make(map[string]Var)

	sv := RegisterStringVar("LIVE_TEST", "initial", "test", ComponentTesting)

	if sv.Get() != "initial" {
		t.Errorf("expected initial default")
	}

	os.Setenv("LIVE_TEST", "changed")
	defer os.Unsetenv("LIVE_TEST")

	if sv.Get() != "changed" {
		t.Errorf("expected live value 'changed', got %q", sv.Get())
	}
}
