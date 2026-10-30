---
name: given-when-assert-that
description: Design and write Go behavior tests as readable Given/When/AssertThat scenarios with observable assertions, using ordinary Go tests without a BDD framework.
---

# Given/When/AssertThat Go tests

Use this skill when writing Go behavior tests, including tests for new features and bug fixes. Keep the tests executable with `go test`. Do not add a BDD runner, `.feature` files, code generation, or a generic step registry.

## Choose behavior before writing test code

- Identify the user-visible contract and the boundary that proves it: an HTTP or gRPC response, handler reachability, caller identity, persisted state, or emitted event. Do not test only a helper when the behavior depends on listener wiring, serialization, or persistence.
- Start with a working case and the few failure or boundary cases that could change the outcome. Include missing, empty, malformed, and fallback inputs only when the contract treats them differently. Set up any state that a negative assertion depends on, so the test cannot pass against an empty or unwired system.
- For each case, write down `Given <relevant state>, when <one action>, assert that <observable outcome>`. Make the assertion specific enough to fail if the behavior is absent; avoid tests that just check constants, mocks, or implementation steps.

## Write the scenario as an ordinary Go test

- Give each test function a short behavior name. When subtests help distinguish cases, give each `t.Run` a sentence: `Given <precondition>, when <one action with its input variant>, assert that <observable outcome>`. Do not add a subtest or fixture just to satisfy the naming pattern.
- Use an **eventive When**, not a stative one. "When a caller sends an RPC without a token" names the stimulus. "When an RPC has no credentials" describes state. Put stable state in Given.
- Make the name match the actual input. "No valid token" includes missing, malformed, and expired tokens. Do not use it for a case that sends only a missing token.
- Keep variants in a `scenarios` matrix when they share an action and assertion. Give distinct behaviors separate named subtests. Do not branch assertions on expected outcomes to force unlike cases into one table.

## Keep the invocation readable

Keep setup local for a single case. When several cases share substantial setup or transport mechanics, put test functions and scenario matrices above reusable helpers and create a fresh fixture inside each subtest unless shared state is immutable and the cases cannot interfere.

```go
func TestProtectedEndpoint(t *testing.T) {
	t.Run("Given a protected endpoint, when a request has no token, assert that dispatch is rejected", func(t *testing.T) {
		newAuthFixture(t).
			GivenProtectedEndpoint().
			WhenRequestWithoutCredentials().
			AssertThatRequestIsRejectedBeforeDispatch()
	})
}
```

`Given` sets up the invariant, `When` performs the action, and `AssertThat` checks the result at the boundary. Keep an existing Options-based fixture setup when it makes configuration choices explicit; do not flatten it into raw transport calls for presentation. Pair this rejection case with a valid-token case proving the handler is reachable; otherwise a permanently unwired handler could pass the negative test. Use stage methods only when they remove repeated mechanics.

If updating an existing test, preserve its behavior, assertions, cleanup, and meaningful edge cases. Do not rewrite neighboring tests merely for presentation.

## Check that the tests prove the contract

Run `gofmt` on changed Go files and the smallest `go test` command that exercises every new case. Run the repository's applicable lint command. For a regression test, confirm it fails against the broken behavior and passes with the fix; for a new feature, confirm each assertion observes the intended boundary rather than a fixture's own return value. Replace `/` in dynamic names so `testing.T.Run` does not split a sentence into nested subtests.
