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
	destination, err := filepath.Abs(options.Directory)
	if err != nil {
		return "", fmt.Errorf("failed to resolve export directory: %w", err)
	}
	if _, err := os.Lstat(destination); err == nil {
		return "", fmt.Errorf("export directory already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to inspect export directory: %w", err)
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
	if err := validateInstallConfig(cfg); err != nil {
		return "", err
	}
	for _, chart := range []Chart{cfg.Kagent, cfg.KagentCRDs, cfg.Substrate, cfg.SubstrateCRDs, cfg.PodCertificate} {
		if chart.Version == "" || chart.Version == "dev" {
			return "", fmt.Errorf("chart %s requires an explicit released version in the CLI configuration", chart.Name)
		}
	}
	if cfg.APIKey != "" {
		path := []string{"providers", modelProviderKey(cfg.ModelProvider), "apiKey"}
		found := false
		for _, ref := range options.Secrets {
			if ref.Release == "kagent" && slices.Equal(ref.Path, path) {
				cfg.APIKey = "${" + ref.Environment + "}"
				found = true
			}
		}
		if !found {
			return "", fmt.Errorf("model API key requires an explicit environment reference for export")
		}
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
		{"kagent-crds", &cfg.KagentCRDs}, {"substrate-crds", &cfg.SubstrateCRDs},
		{podCertificateRelease, &cfg.PodCertificate}, {"substrate", &cfg.Substrate}, {"kagent", &cfg.Kagent},
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
	if err := os.WriteFile(filepath.Join(staging, "README.md"), []byte(exportReadme(cfg, options.Secrets)), 0o644); err != nil {
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

func exportReadme(cfg Config, refs []SecretReference) string {
	var readme strings.Builder
	readme.WriteString("# Helm installation export\n\nThis directory contains Helm commands and the complete installer-supplied overrides for each chart. Chart defaults are not included.\n\nReview every values file before committing this directory. User-provided values are included as supplied; arbitrary secrets are not automatically detected or sanitized. Only the explicit environment references below replace credential values.\n\n## Before running\n\nFollow the Helm installation documentation to complete the prerequisite steps. This export contains only Helm commands and values files; it does not perform prerequisite setup.\n\n")
	if cfg.SkipDatabaseSetup {
		readme.WriteString("Generated with `--skip-database-setup`: the values expect your configured database.\n\n")
	} else {
		readme.WriteString("The values include the bundled database's certificate authentication settings. This export does not provision the database.\n\n")
	}
	if len(refs) > 0 {
		readme.WriteString("## Environment references\n\nSupply these variables before running `install.sh`. The script binds them with Helm `--set-file` using temporary private files; Helm does not expand placeholders in YAML.\n\n")
		for _, ref := range refs {
			fmt.Fprintf(&readme, "- `%s`: release `%s`, value `%s`.\n", ref.Environment, ref.Release, helmMapPath(ref.Path))
		}
		readme.WriteByte('\n')
	}
	readme.WriteString("## Apply\n\n```sh\nsh ./install.sh\n```\n\nCommands run in installation order and use the current Helm/Kubernetes context. Re-running them upgrades the same releases. Previewing this directory did not check the cluster or calculate a resource diff.\n")
	return readme.String()
}
