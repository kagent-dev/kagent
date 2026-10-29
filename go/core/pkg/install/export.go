package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"sigs.k8s.io/yaml"
)

// SecretReference identifies a credential owned by the caller. Path contains map
// keys, not a dotted Helm expression. Arbitrary values are not inspected for secrets.
type SecretReference struct {
	Release     string
	Path        []string
	Environment string
}

type ExportOptions struct {
	Directory string
	Secrets   []SecretReference
	// APIKeyEnvironment names the runtime variable for the selected model provider.
	// Export constructs its credential reference; Secrets describes additional credentials.
	APIKeyEnvironment string
	// ReadmeTemplate contains the required Go text/template source for README.md.
	// Templates expose SkipDatabaseSetup, Secrets (Release, Path, Environment), and Data.
	ReadmeTemplate string
	// ReadmeData is optional caller-provided data exposed as .Data in ReadmeTemplate.
	ReadmeData any
}

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var shellWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// Export writes Helm commands and merged overrides without Kubernetes access.
// Only explicitly referenced credentials are replaced; other values are included
// as supplied. The destination must not exist, including as a symbolic link.
func Export(ctx context.Context, runner Runner, cfg Config, options ExportOptions) (directory string, resultErr error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if options.Directory == "" {
		return "", fmt.Errorf("export directory is required")
	}
	if strings.TrimSpace(options.ReadmeTemplate) == "" {
		return "", fmt.Errorf("README template is required")
	}
	destination, err := filepath.Abs(options.Directory)
	if err != nil {
		return "", fmt.Errorf("failed to resolve export directory: %w", err)
	}
	if _, err := os.Lstat(destination); err == nil {
		return "", fmt.Errorf("export directory already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to inspect export directory: %w", err)
	}
	if err := validateInstallConfig(cfg); err != nil {
		return "", err
	}
	if options.APIKeyEnvironment != "" {
		cfg.APIKey = "${" + options.APIKeyEnvironment + "}"
		options.Secrets = append(slices.Clone(options.Secrets), SecretReference{
			Release: kagentRelease, Path: []string{"providers", modelProviderKey(cfg.ModelProvider), "apiKey"}, Environment: options.APIKeyEnvironment,
		})
	} else if cfg.APIKey != "" {
		return "", fmt.Errorf("model API key requires APIKeyEnvironment for export")
	}
	for i, ref := range options.Secrets {
		if ref.Release == "" || len(ref.Path) == 0 || !environmentName.MatchString(ref.Environment) || slices.Contains(ref.Path, "") {
			return "", fmt.Errorf("secret reference %d requires a release, map path and valid environment variable", i+1)
		}
		for _, previous := range options.Secrets[:i] {
			if previous.Release == ref.Release && slices.Equal(previous.Path, ref.Path) {
				return "", fmt.Errorf("duplicate secret reference for release %s", ref.Release)
			}
		}
	}
	for _, chart := range []Chart{cfg.Kagent, cfg.KagentCRDs, cfg.Substrate, cfg.SubstrateCRDs, cfg.PodCertificate} {
		if chart.Version == "" || chart.Version == "dev" {
			return "", fmt.Errorf("chart %s requires an explicit released version in the CLI configuration", chart.Name)
		}
	}
	readme, err := exportReadme(cfg.SkipDatabaseSetup, options.Secrets, options.ReadmeTemplate, options.ReadmeData)
	if err != nil {
		return "", err
	}
	work, err := os.MkdirTemp("", "kagent-install-export-")
	if err != nil {
		return "", fmt.Errorf("failed to create export workspace: %w", err)
	}
	staging := ""
	defer func() {
		for _, path := range []string{work, staging} {
			if path != "" {
				if err := os.RemoveAll(path); err != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("failed to clean export workspace: %w", err))
				}
			}
		}
	}()
	valuesChart := filepath.Join(work, "merge")
	if err := writeValuesChart(valuesChart); err != nil {
		return "", err
	}
	// Snapshot caller files before planning, but retain --set inputs: folding
	// those into files would let generated --set defaults override them.
	charts := []struct {
		release string
		chart   *Chart
	}{
		{kagentCRDsRelease, &cfg.KagentCRDs}, {substrateCRDsRelease, &cfg.SubstrateCRDs},
		{podCertificateRelease, &cfg.PodCertificate}, {substrateRelease, &cfg.Substrate}, {kagentRelease, &cfg.Kagent},
	}
	for _, item := range charts {
		// Keep local chart references usable when the script runs elsewhere.
		if info, err := os.Stat(chartReference(*item.chart)); err == nil && info.IsDir() {
			repository, err := filepath.Abs(item.chart.Repository)
			if err != nil {
				return "", fmt.Errorf("failed to resolve local chart directory: %w", err)
			}
			item.chart.Repository = repository
		}
		files := *item.chart
		files.Values = nil
		values, err := mergeValues(ctx, runner, files, valuesChart)
		if err != nil {
			return "", err
		}
		if err := replaceSecrets(values, item.release, options.Secrets, false); err != nil {
			return "", err
		}
		path := filepath.Join(work, item.release+".yaml")
		if err := writeValues(path, values); err != nil {
			return "", err
		}
		item.chart.inlineValues = ""
		item.chart.ValuesFiles = []string{path}
	}
	plan, err := planInstall(ctx, runner, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to plan Helm export: %w", &valuesError{cause: err})
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("failed to create export parent directory: %w", err)
	}
	staging, err = os.MkdirTemp(parent, ".kagent-install-")
	if err != nil {
		return "", fmt.Errorf("failed to create export directory: %w", err)
	}
	if err := os.Mkdir(filepath.Join(staging, "values"), 0o755); err != nil {
		return "", fmt.Errorf("failed to create values directory: %w", err)
	}
	var releases []ChartInstall
	for _, step := range plan.steps {
		release, ok := step.(ChartInstall)
		if !ok {
			continue
		}
		values, err := mergeValues(ctx, runner, release.Chart, valuesChart)
		if err != nil {
			return "", err
		}
		if err := replaceSecrets(values, release.Release, options.Secrets, true); err != nil {
			return "", err
		}
		if err := writeValues(filepath.Join(staging, "values", release.Release+".yaml"), values); err != nil {
			return "", err
		}
		releases = append(releases, release)
	}
	for _, ref := range options.Secrets {
		if !slices.ContainsFunc(releases, func(s ChartInstall) bool { return s.Release == ref.Release }) {
			return "", fmt.Errorf("secret reference names an unplanned release: %s", ref.Release)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "install.sh"), []byte(exportScript(releases, options.Secrets)), 0o755); err != nil {
		return "", fmt.Errorf("failed to write Helm script: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "README.md"), []byte(readme), 0o644); err != nil {
		return "", fmt.Errorf("failed to write export instructions: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(staging, destination); err != nil {
		return "", fmt.Errorf("failed to publish export directory: %w", err)
	}
	return destination, nil
}

func writeValues(path string, values map[string]any) error {
	data, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("failed to encode export values: %w", &valuesError{cause: err})
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write export values: %w", err)
	}
	return nil
}

func replaceSecrets(values map[string]any, release string, refs []SecretReference, required bool) error {
	for _, ref := range refs {
		if ref.Release != release {
			continue
		}
		current := values
		found := true
		for _, key := range ref.Path[:len(ref.Path)-1] {
			value, exists := current[key]
			if !exists {
				found = false
				break
			}
			next, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("secret reference for %s must follow a map path", release)
			}
			current = next
		}
		key := ref.Path[len(ref.Path)-1]
		if _, exists := current[key]; !exists || !found {
			if required {
				return fmt.Errorf("secret reference path is missing from release %s", release)
			}
			continue
		}
		current[key] = "${" + ref.Environment + "}"
	}
	return nil
}

func exportScript(releases []ChartInstall, refs []SecretReference) string {
	var script strings.Builder
	script.WriteString("#!/bin/sh\nset -eu\n\n# Complete the prerequisites in README.md before running these commands.\nscript_dir=$(CDPATH= cd -- \"$(dirname -- \"$0\")\" && pwd)\n")
	var environments []string
	for _, ref := range refs {
		if !slices.Contains(environments, ref.Environment) {
			environments = append(environments, ref.Environment)
			fmt.Fprintf(&script, ": \"${%s:?Set %s before running this script}\"\n", ref.Environment, ref.Environment)
		}
	}
	if len(environments) > 0 {
		script.WriteString("umask 077\nsecret_dir=$(mktemp -d)\ntrap 'rm -rf \"$secret_dir\"' EXIT\ntrap 'exit 1' HUP INT TERM\n")
		for _, name := range environments {
			fmt.Fprintf(&script, "printf '%%s' \"${%s}\" > \"$secret_dir/%s\"\n", name, name)
		}
	}
	for _, release := range releases {
		release.Chart.Values, release.Chart.ValuesFiles, release.Chart.inlineValues = nil, nil, ""
		command := release.Command()
		script.WriteString("\n" + shellQuote(command.Program))
		for _, arg := range command.Args {
			script.WriteString(" " + shellQuote(arg))
		}
		fmt.Fprintf(&script, " -f \"$script_dir/values/%s.yaml\"", release.Release)
		for _, ref := range refs {
			if ref.Release == release.Release {
				fmt.Fprintf(&script, " --set-file %s\"$secret_dir/%s\"", shellQuote(helmMapPath(ref.Path)+"="), ref.Environment)
			}
		}
		script.WriteByte('\n')
	}
	return script.String()
}

func shellQuote(value string) string {
	if shellWord.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func helmMapPath(keys []string) string {
	escape := strings.NewReplacer("\\", "\\\\", ".", "\\.", "[", "\\[", "]", "\\]", ",", "\\,", "=", "\\=")
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = escape.Replace(key)
	}
	return strings.Join(parts, ".")
}

type readmeData struct {
	SkipDatabaseSetup bool
	Secrets           []readmeSecret
	Data              any
}

type readmeSecret struct {
	Release     string
	Path        string
	Environment string
}

func exportReadme(skipDatabaseSetup bool, refs []SecretReference, source string, callerData any) (string, error) {
	tmpl, err := template.New("README.md").Option("missingkey=error").Parse(source)
	if err != nil {
		return "", fmt.Errorf("failed to parse export README template: %w", err)
	}
	data := readmeData{SkipDatabaseSetup: skipDatabaseSetup, Data: callerData}
	for _, ref := range refs {
		data.Secrets = append(data.Secrets, readmeSecret{Release: ref.Release, Path: helmMapPath(ref.Path), Environment: ref.Environment})
	}
	var readme strings.Builder
	if err := tmpl.Execute(&readme, data); err != nil {
		return "", fmt.Errorf("failed to render export README: %w", err)
	}
	return readme.String(), nil
}
