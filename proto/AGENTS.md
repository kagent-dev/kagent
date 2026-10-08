# Protobuf contracts

Kagent-owned contracts live under `kagent/api/v1alpha1/`. Their version is
independent of the `v1alpha3` Kubernetes APIs.

- Keep lifecycle and catalog APIs separate from upstream A2A interaction APIs.
- Declare request-intrinsic validation in source `.proto` files with `buf.validate`.
  Use standard rules first, CEL for cross-field or domain rules, and reusable
  predefined rules only when shared across schemas.
- The shared gRPC Protovalidate interceptor enforces those annotations. Authorization
  and checks requiring database, Kubernetes, or network state belong in services.
- Go validation rules live in generated descriptors; there are no generated Go
  validator files. Never hand-edit generated clients or servers.
- Preserve the upstream A2A, Substrate, and guest schemas. Follow [README.md](README.md)
  for dependency updates; do not change vendored schemas independently.
- From the repository root, run `make proto-lint proto-generate proto-breaking`.
  `make proto-generate` updates Go, TypeScript, and Python outputs; include all
  affected outputs. `make proto-check` verifies committed generated output in CI.
- Cover valid and invalid inputs in the affected validation tests, including
  [gRPC interceptor tests](../go/core/internal/grpcserver/protovalidate_test.go).
  New public behavior also needs API E2E coverage.
