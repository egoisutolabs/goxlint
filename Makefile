# `make verify` is the full verification gate: the same checks CI runs (see
# AGENTS.md). Score runs it on a staged merge tree before landing any PR, and
# a nonzero exit blocks the merge.
.PHONY: verify fmt-check vet test selflint

verify: fmt-check vet test selflint

fmt-check:
	@unformatted="$$(gofmt -l .)" || exit 1; \
	if [ -n "$$unformatted" ]; then echo "These files need gofmt:"; echo "$$unformatted"; exit 1; fi

vet:
	go vet ./...

test:
	go test -race ./...

# goxlint lints itself; any diagnostic fails the gate.
selflint:
	go run ./examples/goxlint ./...
