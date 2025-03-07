from typing import Annotated, Optional

from autogen_core.tools import FunctionTool

from .._utils import create_typed_fn_tool
from ..common.shell import run_command


def _helm_list_releases(
    namespace: Annotated[Optional[str], "The namespace to list the helm charts from"] = None,
    all_namespaces: Annotated[Optional[bool], "If true, list releases from all namespaces"] = False,
    all: Annotated[Optional[bool], "If true, show all releases without any filter applied"] = False,
    uninstalled: Annotated[Optional[bool], "If true, list uninstalled releases"] = False,
    uninstalling: Annotated[Optional[bool], "If true, list uninstalling releases"] = False,
    failed: Annotated[Optional[bool], "If true, list failed releases"] = False,
    deployed: Annotated[Optional[bool], "If true, list deployed releases"] = False,
    filter: Annotated[
        Optional[str],
        "a regular expression (Perl compatible). Any releases that match the expression will be included in the results",
    ] = None,
    pending: Annotated[Optional[bool], "If true, list pending releases"] = False,
    output: Annotated[
        Optional[str],
        "The output format of the helm list command, one of: table, json, yaml. Prefer table for human readability.",
    ] = None,
) -> str:
    args: list[str] = []
    if namespace:
        args.append(f"-n {namespace}")

    if all_namespaces:
        args.append("-A")

    if all:
        args.append("-a")

    if uninstalled:
        args.append("--uninstalled")

    if uninstalling:
        args.append("--uninstalling")

    if failed:
        args.append("--failed")

    if deployed:
        args.append("--deployed")

    if pending:
        args.append("--pending")

    if output:
        args.append(f"-o {output}")

    if filter:
        args.append(f"-f {filter}")

    return _run_helm_command(f"list {' '.join(args)}")


helm_list_releases = FunctionTool(
    _helm_list_releases,
    description="""
This command lists all of the releases for a specified namespace (uses current namespace context if namespace not specified).


If the --filter flag is provided, it will be treated as a filter. Filters are
regular expressions (Perl compatible) that are applied to the list of releases.
Only items that match the filter will be returned.

    $ helm list --filter 'ara[a-z]+'
    NAME                UPDATED                                  CHART
    maudlin-arachnid    2020-06-18 14:17:46.125134977 +0000 UTC  alpine-0.1.0
    """,
    name="helm_list_releases",
)

ListReleases, ListReleasesConfig = create_typed_fn_tool(
    helm_list_releases, "kagent.tools.helm.ListReleases", "ListReleases"
)


def _helm_get_release(
    name: Annotated[str, "The name of the release"],
    namespace: Annotated[str, "The namespace to get the release from"],
    resource: Annotated[
        str,
        "The resource to get information about. If not provided, all resources will be returned, can be one of: all, hooks, manifest, notes, values",
    ],
) -> str:
    return _run_helm_command(f"get {resource} {name} -n {namespace}")


helm_get_release = FunctionTool(
    func=_helm_get_release,
    description="""
This command consists of multiple subcommands which can be used to
get extended information about the release, including:


Available specifiers:
  all         download all information for a named release
  hooks       download all hooks for a named release
  manifest    download the manifest for a named release. The manifest is a YAML-formatted
               file containing the complete state of the release.
  notes       download the notes for a named release. The notes are a text
               document that contains information about the release.
  values      download the values file for a named release. The values are a
               YAML-formatted file containing the values used to generate the
               release.
""",
    name="helm_get_release",
)

GetRelease, GetReleaseConfig = create_typed_fn_tool(helm_get_release, "kagent.tools.helm.GetRelease", "GetRelease")


def _upgrade_release(
    name: Annotated[str, "The name of the release"],
    chart: Annotated[str, "The chart to install"],
    namespace: Annotated[str, "The namespace to install the release in"],
    create_namespace: Annotated[Optional[bool], "If true, create the namespace if it does not exist"] = False,
    set: Annotated[
        Optional[list[str]],
        "A list of key-value pairs to set on the release. (can specify multiple or separate values with commas: key1=val1,key2=val2). They will be merged in the order they are provided.",
    ] = None,
    values: Annotated[
        Optional[list[str]],
        "A list of files to use as the value source. (can specify multiple or separate values with commas: myvalues.yaml,override.yaml). They will be merged in the order they are provided.",
    ] = None,
    version: Annotated[Optional[str], "The version of the chart to install"] = None,
    install: Annotated[Optional[bool], "If true, install the release if it does not exist"] = False,
    dry_run: Annotated[
        Optional[bool], "If true, show which releases will be uninstalled without actually uninstalling them"
    ] = False,
    wait: Annotated[Optional[bool], "If true, wait for the release to be deployed"] = False,
) -> str:
    args: list[str] = [name, chart, "-n", namespace]
    if create_namespace:
        args.append("--create-namespace")
    if set:
        args.append(f"--set {','.join(set)}")
    if values:
        args.append(f"--values {','.join(values)}")
    if version:
        args.append(f"--version {version}")
    if install:
        args.append("--install")
    if dry_run:
        args.append("--dry-run")
    if wait:
        args.append("--wait")

    return _run_helm_command(f"upgrade {' '.join(args)}")


upgrade_release = FunctionTool(
    _upgrade_release,
    description="""
This command upgrades or installs a release to a new version of a chart.

The upgrade arguments must be a release and chart. The chart
argument can be either: a chart reference('example/mariadb'), a path to a chart directory,
a packaged chart, or a fully qualified URL. For chart references, the latest
version will be specified unless the '--version' flag is set.

There are six different ways you can express the chart you want to install:

1. By chart reference: helm install mymaria example/mariadb
2. By path to a packaged chart: helm install mynginx ./nginx-1.2.3.tgz
3. By path to an unpacked chart directory: helm install mynginx ./nginx
4. By absolute URL: helm install mynginx https://example.com/charts/nginx-1.2.3.tgz
5. By chart reference and repo url: helm install --repo https://example.com/charts/ mynginx nginx
6. By OCI registries: helm install mynginx --version 1.2.3 oci://example.com/charts/nginx
""",
    name="helm_upgrade_release",
)

Upgrade, UpgradeConfig = create_typed_fn_tool(upgrade_release, "kagent.tools.helm.Upgrade", "Upgrade")


def _helm_uninstall(
    name: Annotated[str, "The name of the release"],
    namespace: Annotated[str, "The namespace to uninstall the release from"],
    dry_run: Annotated[
        Optional[bool], "If true, show which releases will be uninstalled without actually uninstalling them"
    ] = False,
    wait: Annotated[Optional[bool], "If true, wait for the release to be uninstalled"] = False,
) -> str:
    args: list[str] = [name, "-n", namespace]
    if dry_run:
        args.append("--dry-run")
    if wait:
        args.append("--wait")
    return _run_helm_command(f"uninstall {' '.join(args)}")


helm_uninstall = FunctionTool(
    _helm_uninstall,
    description="""
This command takes a release name and uninstalls the release.

It removes all of the resources associated with the last release of the chart
as well as the release history, freeing it up for future use.

Use the '--dry-run' flag to see which releases will be uninstalled without actually
uninstalling them.

Usage:
  helm uninstall RELEASE_NAME [...] [flags]

""",
    name="helm_uninstall",
)


Uninstall, UninstallConfig = create_typed_fn_tool(helm_uninstall, "kagent.tools.helm.Uninstall", "Uninstall")


def _repo_update() -> str:
    return _run_helm_command("repo update")


helm_repo_update = FunctionTool(
    _repo_update,
    description="""
This command updates the local helm repositories.
""",
    name="helm_repo_update",
)

RepoUpdate, RepoUpdateConfig = create_typed_fn_tool(helm_repo_update, "kagent.tools.helm.RepoUpdate", "RepoUpdate")


def _repo_add(name: Annotated[str, "The name of the repository"], url: Annotated[str, "The url of the repository"]):
    return _run_helm_command(f"repo add {name} {url}")


helm_repo_add = FunctionTool(
    _repo_add,
    description="""
This command adds a repository to the local helm repositories.
""",
    name="helm_repo_add",
)

RepoAdd, RepoAddConfig = create_typed_fn_tool(helm_repo_add, "kagent.tools.helm.RepoAdd", "RepoAdd")


def _run_helm_command(command: str) -> str:
    # Split the command and remove empty strings
    cmd_parts = command.split(" ")
    cmd_parts = [part for part in cmd_parts if part]  # Remove empty strings from the list
    return run_command("helm", cmd_parts)
