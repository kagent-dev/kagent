group "default" {
  targets = ["go", "python", "ui"]
}

target "go" {
  context = "./go"
  dockerfile = "Dockerfile"
  args = {
    TOOLS_GO_VERSION = "1.24.3",
    TOOLS_NODE_VERSION = "22",
    TOOLS_UV_VERSION = "0.7.2",
    TOOLS_K9S_VERSION = "0.50.4",
    TOOLS_KIND_VERSION = "0.27.0",
    TOOLS_ISTIO_VERSION = "1.25.2",
    TOOLS_KUBECTL_VERSION = "1.33.4"
  }
  tags = ["controller:latest"]
}

target "python" {
  context = "./python"
  dockerfile = "Dockerfile"
  args = {
    TOOLS_GO_VERSION = "1.24.3",
    TOOLS_NODE_VERSION = "22",
    TOOLS_UV_VERSION = "0.7.2",
    TOOLS_K9S_VERSION = "0.50.4",
    TOOLS_KIND_VERSION = "0.27.0",
    TOOLS_ISTIO_VERSION = "1.25.2",
    TOOLS_KUBECTL_VERSION = "1.33.4"
  }
  tags = ["app:latest"]
}

target "ui" {
  context = "./ui"
  dockerfile = "Dockerfile"
  args = {
    TOOLS_GO_VERSION = "1.24.3",
    TOOLS_NODE_VERSION = "22",
    TOOLS_UV_VERSION = "0.7.2",
    TOOLS_K9S_VERSION = "0.50.4",
    TOOLS_KIND_VERSION = "0.27.0",
    TOOLS_ISTIO_VERSION = "1.25.2",
    TOOLS_KUBECTL_VERSION = "1.33.4"
  }
  tags = ["ui:latest"]
}