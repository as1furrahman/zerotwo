## Description

Please provide a summary of your change, the motivation behind it, and the problem it solves.

---

## Type of Change

- [ ] Bug fix (non-breaking change fixing an issue)
- [ ] New feature (non-breaking change adding functionality)
- [ ] Security hardening / authorization improvement
- [ ] Documentation update
- [ ] Architecture / performance optimization

---

## Checklist

- [ ] My code adheres to the **Zero External Dependencies** rule (`go.mod` has 0 third-party packages).
- [ ] I have formatted my code with `gofmt` and verified `go vet ./...` passes.
- [ ] I have added unit tests for my changes.
- [ ] All tests pass cleanly (`make test`).
- [ ] If this PR introduces or modifies a tool, it is registered in `pkg/auth/policy.go` with strict authorization tiers.
- [ ] No API keys, credentials, or personal tokens are included in the diff.
