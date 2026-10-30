# Exporting an installation

`kagent install --dry-run` writes Helm commands and merged values to `./kagent-install` without changing the cluster. Use `--output-dir` to choose another directory. The destination must not already exist.

```sh
kagent --config config.yaml install --dry-run --output-dir ./kagent-install
```

The directory contains `install.sh`, `README.md`, and one YAML file per release under `values/`. The script installs the releases in their original order. It uses the configured chart references and versions, including local chart paths. It does not provision the database or Substrate prerequisites.

Local chart directories become absolute references so the script can run from another working directory. Those directories must still be available when you apply the export.

Export uses the same planner and Helm values precedence as normal installation. Profile and configured values files remain below `--set` overrides until planning finishes, then the exporter merges the resolved overrides into one file per release. Chart defaults are not included. The export no longer depends on the input values files.

Dry-run requires Helm and access to the configured charts. It does not require kubectl, a kubeconfig, or a model API-key value. Development builds need explicit released chart versions in the CLI configuration. The export is an installation recipe, not a diff against the current cluster.

## Credentials

The CLI replaces its model API key with a named environment reference. Set that variable before running the script:

```sh
export OPENAI_API_KEY='your-key'
sh ./kagent-install/install.sh
```

The script passes the value through Helm `--set-file` using a private temporary file and removes that file when it exits. It does not use `envsubst`; quotes, punctuation, and newlines in the credential do not become YAML or shell syntax.

User-provided values are exported as supplied. The exporter does not infer which arbitrary chart fields contain secrets. Review the values files before committing or sharing the directory. Library callers select the model API-key variable through `ExportOptions.APIKeyEnvironment` and identify additional credentials through `ExportOptions.Secrets`.

## README template

Library callers supply Go `text/template` source through the required `ExportOptions.ReadmeTemplate` field. The CLI embeds [install_readme.md.tmpl](../go/core/cli/internal/commands/install_readme.md.tmpl).

Templates receive `.SkipDatabaseSetup` and `.Secrets` (each with `.Release`, `.Path`, and `.Environment`). Optional `ReadmeData` accepts caller-provided structs or maps, available under `.Data`, such as `{{.Data.DocsURL}}`.

## Prerequisites

Follow the [Helm installation documentation](../helm/README.md#installation) to complete the prerequisite steps before applying the exported commands. The export contains no database manifests, SQL, or prerequisite setup scripts. Preserve existing signing pools and databases when updating an installation.

For an operator-managed database, follow [Preparing your own database](../helm/README.md#preparing-your-own-database), configure its connection Secrets and chart values, and export with `--skip-database-setup`:

```sh
kagent --config config.yaml install --dry-run --skip-database-setup --output-dir ./kagent-install
```

Without that flag, the exported values retain the bundled database's certificate authentication settings. The generated README records which database mode was selected.
