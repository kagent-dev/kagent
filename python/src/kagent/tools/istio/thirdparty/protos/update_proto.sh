#!/usr/bin/env sh

if [ -z "$1" ]; then
    echo "Usage: $0 <workload-telemetry-proto-path>"
    echo "Generate Python gRPC code from workload-telemetry proto file"
    echo ""
    echo "Arguments:"
    echo "  proto-path Path to directory containing the 'workload-telemetry.proto' file"
    echo ""
    echo "Example:"
    echo "  $0 /path/to/proto/dir"
    exit 1
fi

uv run python -m grpc_tools.protoc -I "$1" --python_out=. --grpc_python_out=. workload-telemetry.proto
