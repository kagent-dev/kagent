{
  "build": {
    "dockerfile": "./Dockerfile",
    "context": "."
  },
  "features": {
    "ghcr.io/devcontainers/features/common-utils:2": {
      "username": "codespace",
      "userUid": "1000",
      "userGid": "1000"
    },
    "ghcr.io/devcontainers/features/node:1": {
      "version": "20"
    },
    "ghcr.io/devcontainers/features/python:1": {
      "version": "3.11.9",
      "installJupyterLab": "false"
    },
    "ghcr.io/devcontainers/features/git:1": {
      "version": "latest",
      "ppa": "false"
    },
    "ghcr.io/devcontainers/features/git-lfs:1": {
      "version": "latest"
    },
    "ghcr.io/devcontainers/features/github-cli:1": {
      "version": "latest"
    }
  },
  "overrideFeatureInstallOrder": [
    "ghcr.io/devcontainers/features/common-utils",
    "ghcr.io/devcontainers/features/git",
    "ghcr.io/devcontainers/features/node",
    "ghcr.io/devcontainers/features/python",
    "ghcr.io/devcontainers/features/git-lfs",
    "ghcr.io/devcontainers/features/github-cli"
  ],
  "remoteUser": "codespace",
  "containerUser": "codespace",
  "onCreateCommand": "${containerWorkspaceFolder}/.devcontainer/bootstrap",
  "customizations": {
    "codespaces": {
      "disableAutomaticConfiguration": true,
      "openFiles": [
        "README.md"
      ]
    },
    "vscode": {
      "extensions": [
        "ms-python.python",
        "ms-toolsai.jupyter",
        "ms-toolsai.prompty"
      ],
      "settings": {
        /*
        NOTE: excluding these Python environments causes Jupyter to select the remaining environment by default
        The default environment will be: /usr/local/python/current/bin/python
        */
        "jupyter.kernels.excludePythonEnvironments": [
          "/usr/local/python/current/bin/python3",
          "/usr/bin/python3",
          "/bin/python3"
        ],
        "workbench.editorAssociations": {
          "*.md": "vscode.markdown.preview.editor"
        }
      }
    }
  }
}