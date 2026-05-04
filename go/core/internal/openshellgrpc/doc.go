// Package openshellgrpc holds generated protobuf types for the OpenShell gateway API (subset used by kagent).
//
// Regenerate openshellpb after editing protos under protos/openshell (repo root):
//
//	PKG=github.com/kagent-dev/kagent/go/core/internal/openshellgrpc/openshellpb
//	protoc -I protos/openshell -I protos \
//	  --go_out=go/core/internal/openshellgrpc/openshellpb --go_opt=paths=source_relative \
//	  --go_opt=Mdatamodel.proto=$PKG --go_opt=Msandbox.proto=$PKG --go_opt=Mopenshell.proto=$PKG \
//	  --go-grpc_out=go/core/internal/openshellgrpc/openshellpb --go-grpc_opt=paths=source_relative \
//	  --go-grpc_opt=Mdatamodel.proto=$PKG --go-grpc_opt=Msandbox.proto=$PKG --go-grpc_opt=Mopenshell.proto=$PKG \
//	  protos/openshell/datamodel.proto protos/openshell/sandbox.proto protos/openshell/openshell.proto
package openshellgrpc
