# Exporting an installation

`kagent install --dry-run` writes Helm commands and merged values to `./kagent-install` without changing the cluster. Use `--output-dir` to choose another directory. The destination must not already exist.

```sh
kagent --config config.yaml install --dry-run --output-dir ./kagent-install
```

The directory contains `install.sh`, `README.md`, and one YAML file per release under `values/`. The script installs the releases in their original order. It uses the configured chart references and versions, including local chart paths. It does not provision the database or Substrate prerequisites.

Local chart directories become absolute references so the script can run from another working directory. Those directories must still be available when you apply the export.

Helm resolves the overrides in the same order as a normal install: inline defaults, configured values files in order, then CLI-generated set values. The exported YAML contains those merged overrides, not the chart's defaults. It no longer depends on the original values files.

Dry-run requires Helm and access to the configured charts. It does not require kubectl, a kubeconfig, or a model API-key value. Development builds need explicit released chart versions in the CLI configuration. The export is an installation recipe, not a diff against the current cluster.

## Credentials

The CLI replaces its model API key with a named environment reference. Set that variable before running the script:

```sh
export OPENAI_API_KEY='your-key'
sh ./kagent-install/install.sh
```

The script passes the value through Helm `--set-file` using a private temporary file and removes that file when it exits. It does not use `envsubst`; quotes, punctuation, and newlines in the credential do not become YAML or shell syntax.

User-provided values are exported as supplied. The exporter does not infer which arbitrary chart fields contain secrets. Review the values files before committing or sharing the directory. Applications calling the library can identify additional credentials through `ExportOptions.Secrets`; only those explicit references are replaced.

## Prerequisites

Complete the same prerequisites required by a direct Helm install. On an existing installation, preserve its signing pools and database. On a fresh cluster, interleave setup with the Helm commands in this order:

1. Install the kagent and Substrate CRD releases.
2. Prepare Substrate namespaces, CA and JWT pools, public certificate bundles, API authentication configuration, and the namespace access label.
3. Install the pod-certificate controller release.
4. Prepare PostgreSQL and its connection Secrets.
5. Install the Substrate and kagent releases.

The first three Helm commands in `install.sh` provide the CRD and pod-certificate releases. When copying individual commands, set `script_dir` to the export directory so their values paths resolve. After prerequisite setup is complete, running the entire script upgrades those releases and continues with the remaining ones.

The shared setup helpers can perform the non-Helm steps from automation that already has a Kubernetes client:

```go
client := install.NewClient(install.CommandRunner{}, kubernetesClient)

// Before the pod-certificate release:
if err := client.PrepareSubstrate(ctx, namespace); err != nil {
    return err
}

// After the pod-certificate release, when using the development database:
database := install.Postgres{Namespace: namespace, Image: postgresImage}
if err := client.PreparePostgres(ctx, database); err != nil {
    return err
}
```

Use the configured `install.bundledPostgresImage` for `postgresImage`. The setup helpers create missing signing keys and preserve existing ones. Keep these operations in the prerequisite workflow; they are not included in the export.

For an operator-managed database, follow [Preparing your own database](../helm/README.md#preparing-your-own-database), configure the required connection Secrets and chart values, and generate the export with `--skip-database-setup`:

```sh
kagent --config config.yaml install --dry-run --skip-database-setup --output-dir ./kagent-install
```

Without that flag, the exported values retain the normal install's certificate authentication settings for the development database. Prepare a matching database before applying them. The generated README records which mode was selected.
