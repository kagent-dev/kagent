#!/usr/bin/env python3
"""Release smoke checks: python3 scripts/release-workflows_test.py (requires PyYAML and Helm)."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile

import yaml


ROOT = Path(__file__).resolve().parents[1]


def load(path):
    return yaml.load((ROOT / path).read_text(), Loader=yaml.BaseLoader)


def run(script, cwd, env, check=True):
    return subprocess.run(
        ["bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", script],
        cwd=cwd, env=os.environ | env, text=True, capture_output=True, check=check,
    )


def main():
    nightly = load(".github/workflows/nightly.yaml")
    tagged = load(".github/workflows/tag.yaml")
    prepare = next(s["run"] for s in nightly["jobs"]["prepare"]["steps"] if s.get("id") == "prepare")
    image = load(".github/actions/publish-image/action.yaml")["runs"]["steps"][-1]
    alias = load(".github/actions/publish-helm/action.yaml")["runs"]["steps"][-1]
    assert "workflow_dispatch" in nightly["on"]
    assert nightly["jobs"]["push-images"]["strategy"]["matrix"] == tagged["jobs"]["push-images"]["strategy"]["matrix"]

    with tempfile.TemporaryDirectory(dir=os.environ["TMPDIR"], prefix="release-test-") as tmp:
        work = Path(tmp)
        bin_dir = work / "bin"
        bin_dir.mkdir()
        for name, body in {
            "gh": 'test "${FAIL_GH:-false}" != true || exit 1\nprintf "%s\\n" "$PREVIOUS_SHA"',
            "git": 'echo "commit included in nightly"',
            "make": 'printf "%s\\n" "$*" "${DOCKER_BUILD_ARGS:-}"',
        }.items():
            executable = bin_dir / name
            executable.write_text("#!/usr/bin/env bash\nset -eu\n" + body + "\n")
            executable.chmod(0o755)
        output = work / "output"
        summary = work / "summary"
        # Leading zeros in a numeric SHA must still produce valid Helm SemVer.
        sha = "0123456789012345678901234567890123456789"
        version = "0.0.0-alpha.g" + sha[:12]
        env = {
            "PATH": str(bin_dir) + os.pathsep + os.environ["PATH"],
            "GITHUB_OUTPUT": str(output), "GITHUB_STEP_SUMMARY": str(summary),
            "GITHUB_SHA": sha, "GITHUB_REPOSITORY": "kagent-dev/kagent",
            "GITHUB_REF_NAME": "main", "VERSION": version,
        }
        for previous, event, expected in [
            ("", "schedule", "true"), (sha, "schedule", "false"),
            ("a" * 40, "schedule", "true"), (sha, "workflow_dispatch", "true"),
        ]:
            output.write_text("")
            run(prepare, work, env | {"PREVIOUS_SHA": previous, "GITHUB_EVENT_NAME": event})
            assert output.read_text().splitlines() == [f"version={version}", f"release={expected}"]
        output.write_text("")
        failed = run(prepare, work, env | {"FAIL_GH": "true"}, check=False)
        assert failed.returncode != 0 and not output.read_text(), "API errors must stop publication"

        resolve = tagged["jobs"]["setup"]["steps"][0]["run"]
        for value in ["v0.10.0", "0.10.0", version]:
            output.write_text("")
            run(resolve, work, env | {"VERSION": value})
            assert output.read_text().strip() == "version=" + value.removeprefix("v")

        for component in nightly["jobs"]["push-images"]["strategy"]["matrix"]["image"]:
            for extra in ["", "latest-dev"]:
                result = run(image["run"], work, env | {
                    "IMAGE": component, "EXTRA_TAG": extra,
                    "DOCKER_BUILD_ARGS": image["env"]["DOCKER_BUILD_ARGS"],
                })
                args = result.stdout.splitlines()
                assert args[0] == f"build-{component}"
                assert "--platform linux/amd64,linux/arm64" in args[1]
                assert (f"-t ghcr.io/kagent-dev/kagent/{component}:latest-dev" in args[1]) == bool(extra)

        # Render the real parent chart without fetching its unrelated subcharts.
        chart = work / "helm/kagent"
        shutil.copytree(ROOT / "helm/kagent", chart, ignore=shutil.ignore_patterns("charts", "Chart.lock", "values.local.yaml"))
        metadata = yaml.safe_load((chart / "Chart-template.yaml").read_text())
        metadata.pop("dependencies")
        for chart_version in [version, "0.0.0-latest-dev"]:
            if chart_version != version:
                result = run(alias["run"], work, env | {"EXTRA_VERSION": chart_version})
                assert result.stdout.splitlines()[0] == f"helm-publish VERSION={chart_version}"
            metadata["version"] = chart_version
            (chart / "Chart.yaml").write_text(yaml.safe_dump(metadata))
            rendered = subprocess.check_output([
                "helm", "template", "kagent", str(chart), "--set", "providers.default=ollama",
            ], text=True)
            resources = list(yaml.safe_load_all(rendered))
            deployments = [r for r in resources if r and r["kind"] == "Deployment"]
            for component in ["controller", "ui"]:
                deployment = next(r for r in deployments if r["metadata"]["name"] == "kagent-" + component)
                assert deployment["spec"]["template"]["spec"]["containers"][0]["image"] == f"ghcr.io/kagent-dev/kagent/{component}:{version}"
            config = next(r for r in resources if r and r["kind"] == "ConfigMap" and "IMAGE_TAG" in r.get("data", {}))
            assert config["data"]["IMAGE_TAG"] == version

    print("Release checks passed: dispatch, skip/retry, versions, image tags and Helm image pinning.")


if __name__ == "__main__":
    main()
