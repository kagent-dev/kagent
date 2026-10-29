package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const testReadmeTemplate = "Skip database: {{.SkipDatabaseSetup}}\n{{if .Secrets}}## Environment references\n{{range .Secrets}}{{.Release}} {{.Path}} {{.Environment}}\n{{end}}{{end}}"

func valuesManifest(values map[string]any) string {
	data, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return "apiVersion: v1\nkind: ConfigMap\ndata:\n  values: " + strconv.Quote(string(data)) + "\n"
}

func exportReferences() []SecretReference {
	return []SecretReference{{Release: "kagent", Path: []string{"providers", "openAI", "apiKey"}, Environment: "OPENAI_API_KEY"}}
}

func exportRunner() *recordingRunner {
	merges := 0
	return &recordingRunner{run: func(cmd Command) (string, error) {
		if cmd.Args[1] != "kagent-install-values" {
			return "kind: Deployment\nspec: {template: {spec: {serviceAccountName: test-controller}}}", nil
		}
		merges++
		if merges == 10 {
			return valuesManifest(map[string]any{"providers": map[string]any{"openAI": map[string]any{"apiKey": "placeholder"}}}), nil
		}
		return valuesManifest(map[string]any{}), nil
	}}
}

func TestExportOnlyWritesHelmArtifacts(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(fmt.Sprintf("skip_database=%t", skip), func(t *testing.T) {
			cfg := testConfig()
			cfg.SkipDatabaseSetup = skip
			dir := filepath.Join(t.TempDir(), "export")
			runner := exportRunner()
			path, err := Export(t.Context(), runner, cfg, ExportOptions{ReadmeTemplate: testReadmeTemplate, Directory: dir, APIKeyEnvironment: "OPENAI_API_KEY"})
			require.NoError(t, err)
			require.Equal(t, dir, path)
			files, err := os.ReadDir(dir)
			require.NoError(t, err)
			var names []string
			for _, file := range files {
				names = append(names, file.Name())
			}
			require.Equal(t, []string{"README.md", "install.sh", "values"}, names)
			values, err := os.ReadDir(filepath.Join(dir, "values"))
			require.NoError(t, err)
			require.Len(t, values, 5)
			for _, cmd := range runner.commands {
				require.Equal(t, "helm", cmd.Program)
				require.Equal(t, "template", cmd.Args[0], "export must never invoke mutating commands")
				require.NotContains(t, strings.Join(cmd.Args, " ")+cmd.Stdin, cfg.APIKey)
			}
			require.Equal(t, "secret-provider-key", cfg.APIKey, "export must not mutate its input")
			require.NoError(t, filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.NotContains(t, string(data), cfg.APIKey)
				return nil
			}))
			readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
			require.NoError(t, err)
			require.Contains(t, string(readme), fmt.Sprintf("Skip database: %t", skip))
			require.Contains(t, string(readme), "kagent providers.openAI.apiKey OPENAI_API_KEY")
		})
	}
}

func TestExportDoesNotOverwriteExistingDestination(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "keep")
	require.NoError(t, os.WriteFile(file, []byte("original"), 0o600))
	runner := exportRunner()
	_, err := Export(t.Context(), runner, testConfig(), ExportOptions{ReadmeTemplate: testReadmeTemplate, Directory: dir})
	require.ErrorContains(t, err, "already exists")
	require.Empty(t, runner.commands)
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(dir, link))
	_, err = Export(t.Context(), runner, testConfig(), ExportOptions{ReadmeTemplate: testReadmeTemplate, Directory: link})
	require.ErrorContains(t, err, "already exists")
}

func TestExportFailuresLeaveNoPartialDirectory(t *testing.T) {
	for _, failAt := range []int{1, 6, 9, 11} {
		t.Run(fmt.Sprintf("command_%d", failAt), func(t *testing.T) {
			parent := t.TempDir()
			runner := exportRunner()
			run := runner.run
			cause := errors.New("secret-from-a-diagnostic")
			calls := 0
			runner.run = func(cmd Command) (string, error) {
				calls++
				if calls == failAt {
					return "secret-from-a-diagnostic", cause
				}
				return run(cmd)
			}
			_, err := Export(t.Context(), runner, testConfig(), ExportOptions{ReadmeTemplate: testReadmeTemplate, Directory: filepath.Join(parent, "export"), APIKeyEnvironment: "OPENAI_API_KEY"})
			require.ErrorIs(t, err, cause)
			require.NotContains(t, err.Error(), "secret-from-a-diagnostic")
			files, err := os.ReadDir(parent)
			require.NoError(t, err)
			require.Empty(t, files)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runner := exportRunner()
	_, err := Export(ctx, runner, testConfig(), ExportOptions{ReadmeTemplate: testReadmeTemplate, Directory: filepath.Join(t.TempDir(), "export")})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, runner.commands)
}

func TestExplicitSecretReferences(t *testing.T) {
	values := map[string]any{"password": "user-supplied", "auth": map[string]any{"api.key": "owned-secret"}}
	ref := SecretReference{Release: "kagent", Path: []string{"auth", "api.key"}, Environment: "OWNED_API_KEY"}
	require.NoError(t, replaceSecrets(values, "kagent", []SecretReference{ref}, true))
	require.Equal(t, "user-supplied", values["password"], "export must not guess which values are secrets")
	require.Equal(t, "${OWNED_API_KEY}", values["auth"].(map[string]any)["api.key"])
	require.Equal(t, `auth.api\.key`, helmMapPath(ref.Path))
	ref.Path = []string{"missing"}
	require.NoError(t, replaceSecrets(values, "kagent", []SecretReference{ref}, false))
	require.ErrorContains(t, replaceSecrets(values, "kagent", []SecretReference{ref}, true), "missing")
}

func TestExportRejectsInvalidSecretReferences(t *testing.T) {
	for _, tc := range []struct {
		name, environment, wantErr string
		refs                       []SecretReference
	}{
		{"missing model environment", "", "model API key requires APIKeyEnvironment", nil},
		{"invalid model environment", "INVALID-NAME", "valid environment variable", nil},
		{"invalid additional environment", "OPENAI_API_KEY", "valid environment variable", []SecretReference{{Release: "kagent", Path: []string{"auth", "token"}, Environment: "INVALID-NAME"}}},
		{"empty path", "OPENAI_API_KEY", "map path", []SecretReference{{Release: "kagent", Path: []string{""}, Environment: "API_KEY"}}},
		{"conflicting model reference", "OPENAI_API_KEY", "duplicate secret reference", exportReferences()},
		{"duplicate additional reference", "OPENAI_API_KEY", "duplicate secret reference", []SecretReference{
			{Release: "kagent", Path: []string{"auth", "token"}, Environment: "TOKEN"},
			{Release: "kagent", Path: []string{"auth", "token"}, Environment: "OTHER_TOKEN"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := exportRunner()
			_, err := Export(t.Context(), runner, testConfig(), ExportOptions{
				ReadmeTemplate: testReadmeTemplate, Directory: filepath.Join(t.TempDir(), "export"), APIKeyEnvironment: tc.environment, Secrets: tc.refs,
			})
			require.ErrorContains(t, err, tc.wantErr)
			require.Empty(t, runner.commands)
		})
	}
}

func TestExportBuildsModelAPIKeyReference(t *testing.T) {
	for _, tc := range []struct{ provider, path string }{
		{"OpenAI", "openAI"},
		{"Anthropic", "anthropic"},
		{"AzureOpenAI", "azureOpenAI"},
		{"Gemini", "gemini"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			cfg := testConfig()
			cfg.ModelProvider, cfg.APIKey = tc.provider, ""
			runner := &recordingRunner{run: func(cmd Command) (string, error) {
				if cmd.Args[1] == "kagent-install-values" {
					return valuesManifest(map[string]any{"providers": map[string]any{tc.path: map[string]any{"apiKey": "placeholder"}}}), nil
				}
				return "kind: Deployment\nspec: {template: {spec: {serviceAccountName: test-controller}}}", nil
			}}
			refs := []SecretReference{{Release: "untouched"}}
			dir := filepath.Join(t.TempDir(), "export")
			_, err := Export(t.Context(), runner, cfg, ExportOptions{
				Directory: dir, APIKeyEnvironment: "CUSTOM_MODEL_KEY", Secrets: refs[:0], ReadmeTemplate: testReadmeTemplate,
			})
			require.NoError(t, err)
			require.Equal(t, "", cfg.APIKey)
			require.Equal(t, "untouched", refs[0].Release, "export must not overwrite the caller's reference backing array")
			readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
			require.NoError(t, err)
			require.Contains(t, string(readme), "kagent providers."+tc.path+".apiKey CUSTOM_MODEL_KEY")
			script, err := os.ReadFile(filepath.Join(dir, "install.sh"))
			require.NoError(t, err)
			require.Contains(t, string(script), "--set-file providers."+tc.path+".apiKey=\"$secret_dir/CUSTOM_MODEL_KEY\"")
			data, err := os.ReadFile(filepath.Join(dir, "values", "kagent.yaml"))
			require.NoError(t, err)
			var values struct {
				Providers map[string]struct{ APIKey string }
			}
			require.NoError(t, yaml.Unmarshal(data, &values))
			require.Equal(t, "${CUSTOM_MODEL_KEY}", values.Providers[tc.path].APIKey)
		})
	}
}

func TestExportValidatesConfigBeforeRendering(t *testing.T) {
	for _, edit := range []func(*Config){
		func(cfg *Config) { cfg.Namespace = "invalid namespace" },
		func(cfg *Config) { cfg.Kagent.Version = "dev" },
	} {
		cfg := testConfig()
		edit(&cfg)
		runner := exportRunner()
		_, err := Export(t.Context(), runner, cfg, ExportOptions{ReadmeTemplate: testReadmeTemplate, Directory: filepath.Join(t.TempDir(), "export"), APIKeyEnvironment: "OPENAI_API_KEY"})
		require.Error(t, err)
		require.Empty(t, runner.commands)
	}
}

func TestExportWithoutCredentialReferences(t *testing.T) {
	cfg := testConfig()
	cfg.ModelProvider, cfg.APIKey = "Bedrock", ""
	dir := filepath.Join(t.TempDir(), "export")
	_, err := Export(t.Context(), exportRunner(), cfg, ExportOptions{ReadmeTemplate: testReadmeTemplate, Directory: dir})
	require.NoError(t, err)
	script, err := os.ReadFile(filepath.Join(dir, "install.sh"))
	require.NoError(t, err)
	require.NotContains(t, string(script), "secret_dir")
	require.NotContains(t, string(script), "--set-file")
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	require.NotContains(t, string(readme), "## Environment references")
}

func TestExportScriptBindsRawSecretsAndCleansTemporaryFiles(t *testing.T) {
	// The shell harness observes arguments and secret files before cleanup. Helm
	// semantics are checked separately against the real executable below.
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	require.NoError(t, os.Mkdir(bin, 0o700))
	argsFile, secretFile, pathFile := filepath.Join(dir, "args"), filepath.Join(dir, "secret"), filepath.Join(dir, "secret-path")
	shim := `#!/bin/sh
printf '%s\n' "$*" >> "$TEST_ARGS_FILE"
for arg in "$@"; do
  case "$arg" in
    providers.openAI.apiKey=*)
      path=${arg#*=}
      printf '%s' "$path" > "$TEST_PATH_FILE"
      cat "$path" > "$TEST_SECRET_FILE"
      ;;
  esac
done
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "helm"), []byte(shim), 0o700))
	output := filepath.Join(dir, "directory with ' quotes")
	cfg := testConfig()
	marker := filepath.Join(dir, "must-not-exist")
	cfg.Kagent.Repository = filepath.Join(dir, "repo'$(touch "+marker+")")
	_, err := Export(t.Context(), exportRunner(), cfg, ExportOptions{ReadmeTemplate: testReadmeTemplate, Directory: output, APIKeyEnvironment: "OPENAI_API_KEY"})
	require.NoError(t, err)
	secret := "key,with=punctuation'\"\\$()\nsecond line"
	cmd := exec.CommandContext(t.Context(), "sh", filepath.Join(output, "install.sh"))
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"OPENAI_API_KEY="+secret, "TEST_ARGS_FILE="+argsFile, "TEST_PATH_FILE="+pathFile, "TEST_SECRET_FILE="+secretFile)
	data, err := cmd.CombinedOutput()
	require.NoError(t, err, string(data))
	require.NotContains(t, string(data), secret)
	captured, err := os.ReadFile(secretFile)
	require.NoError(t, err)
	require.Equal(t, secret, string(captured))
	path, err := os.ReadFile(pathFile)
	require.NoError(t, err)
	_, err = os.Stat(string(path))
	require.True(t, os.IsNotExist(err), "temporary credential files must be removed")
	args, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	require.Len(t, lines, 5)
	for i, release := range []string{"kagent-crds", "substrate-crds", podCertificateRelease, "substrate", "kagent"} {
		require.True(t, strings.HasPrefix(lines[i], "upgrade --install "+release+" "))
	}
	require.NotContains(t, string(args), secret)
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "chart references must not become shell commands")
}

func TestExportMatchesHelmInstallInputs(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for export integration")
	}
	for _, tc := range []struct {
		name                    string
		skipDatabase, overrides bool
	}{
		{"bundled database", false, false},
		{"external database", true, false},
		{"bundled database with caller overrides", false, true},
		{"external database with caller overrides", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			cfg := testConfig()
			cfg.SkipDatabaseSetup = tc.skipDatabase
			for _, chart := range []*Chart{&cfg.Kagent, &cfg.KagentCRDs, &cfg.Substrate, &cfg.SubstrateCRDs, &cfg.PodCertificate} {
				chart.Repository, chart.Version = ".", "0.0.0"
				chart.ValuesFiles = nil
				dir := filepath.Join(root, chart.Name)
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: "+chart.Name+"\nversion: 0.0.0\n"), 0o600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(root, cfg.Kagent.Name, "templates", "controller-deployment.yaml"), []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: controller}\nspec: {template: {spec: {serviceAccountName: {{ .Values.providers.default | lower }}-controller}}}\n"), 0o600))
			file := filepath.Join(root, "operator.yaml")
			require.NoError(t, os.WriteFile(file, []byte("controller: {replicas: 2}\nconfig: {logging: {level: debug}}\nuserValue: retained\n"), 0o600))
			profile := filepath.Join(root, "profile.yaml")
			require.NoError(t, os.WriteFile(profile, []byte("agents: {enabled: false}\n"), 0o600))
			cfg.Kagent.ValuesFiles = []string{profile, file}
			var refs []SecretReference
			const token = "owned-token-must-not-be-exported"
			if tc.overrides {
				cfg.Kagent.Values = []string{"providers.default=ollama", "auth.token=" + token}
				refs = append(refs, SecretReference{Release: "kagent", Path: []string{"auth", "token"}, Environment: "KAGENT_TEST_TOKEN"})
				cfg.PodCertificate.Values = []string{"postgresClients[0]=team/operator=operator_user"}
				cfg.Substrate.Values = []string{"postgres.clientCertificates.enabled=false"}
			}
			runner := CommandRunner{}
			plan, err := planInstall(t.Context(), runner, cfg)
			require.NoError(t, err)
			output := filepath.Join(root, "export")
			_, err = Export(t.Context(), runner, cfg, ExportOptions{ReadmeTemplate: testReadmeTemplate, Directory: output, APIKeyEnvironment: "OPENAI_API_KEY", Secrets: refs})
			require.NoError(t, err)
			script, err := os.ReadFile(filepath.Join(output, "install.sh"))
			require.NoError(t, err)
			require.Contains(t, string(script), filepath.Join(root, "kagent"))
			require.Equal(t, ".", cfg.Kagent.Repository)
			mergeChart := filepath.Join(root, "merge")
			require.NoError(t, writeValuesChart(mergeChart))
			for _, step := range plan.steps {
				release, ok := step.(ChartInstall)
				if !ok {
					continue
				}
				expected, err := mergeValues(t.Context(), runner, release.Chart, mergeChart)
				require.NoError(t, err)
				path := filepath.Join(output, "values", release.Release+".yaml")
				args := []string{"template", "replay", mergeChart, "-f", path}
				for _, ref := range append(exportReferences(), refs...) {
					if ref.Release != release.Release {
						continue
					}
					value := map[string]string{"OPENAI_API_KEY": cfg.APIKey, "KAGENT_TEST_TOKEN": token}[ref.Environment]
					secret := filepath.Join(root, ref.Environment)
					require.NoError(t, os.WriteFile(secret, []byte(value), 0o600))
					args = append(args, "--set-file", helmMapPath(ref.Path)+"="+secret)
				}
				out, err := runner.Run(t.Context(), Command{Program: "helm", Args: args})
				require.NoError(t, err)
				var manifest struct{ Data map[string]string }
				require.NoError(t, yaml.Unmarshal([]byte(out), &manifest))
				var got map[string]any
				require.NoError(t, json.Unmarshal([]byte(manifest.Data["values"]), &got))
				require.Equal(t, expected, got, release.Release)
			}
			// The directory is independent of later changes to input values files.
			require.NoError(t, os.WriteFile(file, []byte("userValue: changed\n"), 0o600))
			data, err := os.ReadFile(filepath.Join(output, "values", "kagent.yaml"))
			require.NoError(t, err)
			require.NotContains(t, string(data), token)
			require.NotContains(t, string(script), token)
			require.Contains(t, string(data), "userValue: retained")
			require.NotContains(t, string(data), "userValue: changed")
		})
	}
}

func TestExportScriptMissingCredentialsStopsBeforeHelm(t *testing.T) {
	refs := exportReferences()
	refs[0].Environment = "KAGENT_TEST_UNSET_CREDENTIAL"
	path := filepath.Join(t.TempDir(), "install.sh")
	require.NoError(t, os.WriteFile(path, []byte(exportScript(nil, refs)), 0o700))
	cmd := exec.CommandContext(t.Context(), "sh", path)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(value string) bool { return strings.HasPrefix(value, "KAGENT_TEST_UNSET_CREDENTIAL=") })
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), "Set KAGENT_TEST_UNSET_CREDENTIAL")
}

func TestExportScriptCleansCredentialsAfterHelmFailure(t *testing.T) {
	dir := t.TempDir()
	bin, temporary := filepath.Join(dir, "bin"), filepath.Join(dir, "temporary")
	require.NoError(t, os.Mkdir(bin, 0o700))
	require.NoError(t, os.Mkdir(temporary, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "helm"), []byte("#!/bin/sh\nexit 7\n"), 0o700))
	path := filepath.Join(dir, "install.sh")
	require.NoError(t, os.WriteFile(path, []byte(exportScript([]ChartInstall{{Release: "kagent", Namespace: "test", Chart: Chart{Name: "kagent", Repository: "./charts", Version: "1"}}}, exportReferences())), 0o700))
	cmd := exec.CommandContext(t.Context(), "sh", path)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "TMPDIR="+temporary, "OPENAI_API_KEY=runtime-only-key")
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.NotContains(t, string(out), "runtime-only-key")
	files, err := os.ReadDir(temporary)
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestExportReadmeTemplate(t *testing.T) {
	for _, tc := range []struct {
		name string
		data any
	}{
		{"struct", struct {
			DocsURL           string
			SkipDatabaseSetup bool
		}{DocsURL: "https://docs.example.test/helm?a=1&b=2"}},
		{"map", map[string]any{"DocsURL": "https://docs.example.test/helm?a=1&b=2", "SkipDatabaseSetup": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.SkipDatabaseSetup = true
			directory := filepath.Join(t.TempDir(), "export")
			_, err := Export(t.Context(), exportRunner(), cfg, ExportOptions{
				Directory:         directory,
				APIKeyEnvironment: "OPENAI_API_KEY",
				ReadmeTemplate:    "# Installation\n\nFollow {{.Data.DocsURL}}.\nSkip database: {{.SkipDatabaseSetup}}\nCaller mode: {{.Data.SkipDatabaseSetup}}\n{{range .Secrets}}{{.Release}} {{.Path}} {{.Environment}}\n{{end}}",
				ReadmeData:        tc.data,
			})
			require.NoError(t, err)
			readme, err := os.ReadFile(filepath.Join(directory, "README.md"))
			require.NoError(t, err)
			require.Equal(t, "# Installation\n\nFollow https://docs.example.test/helm?a=1&b=2.\nSkip database: true\nCaller mode: false\nkagent providers.openAI.apiKey OPENAI_API_KEY\n", string(readme))
			require.NotContains(t, string(readme), cfg.APIKey)
			require.FileExists(t, filepath.Join(directory, "install.sh"))
		})
	}
}

func TestExportReadmeTemplateErrorsLeaveNoOutput(t *testing.T) {
	for _, tc := range []struct{ name, source, wantErr string }{
		{"empty", "", "README template is required"},
		{"whitespace", " \n\t", "README template is required"},
		{"syntax", "{{", "failed to parse export README template"},
		{"missing field", "{{.Unknown}}", "failed to render export README"},
		{"missing caller data key", "{{.Data.Unknown}}", "failed to render export README"},
		{"credential values unavailable", "{{.APIKey}}", "failed to render export README"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			runner := exportRunner()
			cfg := testConfig()
			_, err := Export(t.Context(), runner, cfg, ExportOptions{
				Directory:         filepath.Join(parent, "export"),
				APIKeyEnvironment: "OPENAI_API_KEY",
				ReadmeTemplate:    tc.source,
				ReadmeData:        map[string]any{},
			})
			require.ErrorContains(t, err, tc.wantErr)
			require.NotContains(t, err.Error(), cfg.APIKey)
			require.Empty(t, runner.commands, "invalid templates must fail before Helm runs")
			files, err := os.ReadDir(parent)
			require.NoError(t, err)
			require.Empty(t, files)
		})
	}
}
