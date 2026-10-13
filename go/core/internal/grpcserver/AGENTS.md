# gRPC transport

- Handlers convert wire formats and call the owning service or workflow. Keep
  orchestration, database transactions, locks, and backend details out of handlers.
- Request-intrinsic validation belongs in `.proto` annotations, enforced by the
  shared Protovalidate interceptors. Follow [protobuf guidance](../../../../proto/AGENTS.md)
  when changing contracts. Keep authorization and state-dependent checks in services.
- Register every new RPC in `DefaultMethodPolicies` in `policy.go`. Unconfigured
  methods are denied. Choose access from the operation's effects; test that read-only
  shares cannot invoke mutations and runtime credentials remain scoped correctly.
- Reuse the shared authentication and error-mapping interceptors. Return service
  errors through the existing mapping; do not expose internal errors to callers.
- Test wire conversion, policy, and interceptor behavior here; test business rules
  in the owning service. New endpoints also need API E2E coverage.
- From `go/`, run `go test ./core/internal/grpcserver` and affected service tests.
