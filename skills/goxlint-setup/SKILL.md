---
name: goxlint-setup
description: "Add goxlint to a Go repository. Use when the user wants a linter command, a .goxlintrc.json file, a CI step, or a hook that lints edited Go files."
license: MIT
compatibility: "Requires Go 1.26 or later. The hook scripts need a POSIX shell and jq."
---

# Add goxlint to a repository

goxlint is an engine. You write the rules for this repository. This skill adds the linter command, the configuration, a CI step, and an agent hook.

Read these documents before you change files:

- https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/getting-started.md
- https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/configuration.md
- https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/cli.md

The plugins in the goxlint repository are examples. Write rules for this repository.

## Requirements

1. Run `go version`. Stop when the version is older than Go 1.26.
2. Confirm that the directory has a `go.mod` file.
3. Stop when the repository already has a command that calls `cli.Main`. Tell the user the path.

## 1. Make the linter command

Use the same Go module as the code that the linter examines. The command directory is `tools/lint`.

1. Go to the module root.
2. Read the module path from `go.mod`.
3. Choose a plugin name. Use the last element of the module path. Use lowercase letters, digits, and hyphens. Ask the user when that element contains other characters.
4. Make the directory `tools/lint/rules`.
5. Make `tools/lint/rules/plugin.go`. Replace `billing` with the plugin name.

```go
package rules

import "github.com/egoisutolabs/goxlint"

// Plugin registers the rules of this repository.
// Each rule ID starts with "billing/".
var Plugin = goxlint.Plugin{
	Name: "billing",
}
```

6. Make `tools/lint/main.go`. Replace `example.com/billing` with the module path.

```go
package main

import (
	"example.com/billing/tools/lint/rules"
	"github.com/egoisutolabs/goxlint/cli"
)

func main() { cli.Main(rules.Plugin) }
```

7. Run `go get github.com/egoisutolabs/goxlint`.
8. Run `go build ./tools/lint`.
9. Stop when the build fails. Fix the module path. Build again.

## 2. Write the configuration

1. Run `go run ./tools/lint init` when `.goxlintrc.json` does not exist.
2. When the file exists, do not change it in this step. `init` exits with code 2 and does not write the file.
3. Read `.goxlintrc.json`.

`init` enables the `correctness` category with severity `warn`. It sets each registered rule to `error`. With no rules, the file is:

```json
{
  "categories": {
    "correctness": "warn"
  }
}
```

`init` does not change an existing file. When you add a rule later, add its rule ID to the `rules` object.

## 3. Add a CI step

The linter exits with code 1 when it reports an error. The CI step must fail on that exit code. A warning does not change the exit code. Add `--deny-warnings` when warnings must fail the step.

1. Find the CI configuration.
2. When a step already runs this linter, do not add a second step.
3. For GitHub Actions, add `.github/workflows/goxlint.yml` when no workflow runs the linter:

```yaml
name: goxlint
on: [push, pull_request]
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Lint
        run: go run ./tools/lint ./...
```

4. For another CI system, add the command `go run ./tools/lint ./...`. The step must fail when the exit code is 1.
5. Ask the user when you cannot tell which CI the repository uses.

For GitHub code scanning, read the CI section of https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/cli.md.

## 4. Add an agent hook

The hook lints each edited Go file. It sends the file on stdin. It does not pass a package pattern.

Command:

```sh
go run ./tools/lint --stdin-filename FILE --format json < FILE
```

1. Use this command only for a path that ends with `.go`.
2. Do not give a package pattern with `--stdin-filename`.
3. Do not add a fix flag. A fix flag writes Go source to stdout.
4. Parse stdout as a JSON array. An empty array means no diagnostics.
5. Exit code 0 and an empty array means the file has no diagnostics.
6. Exit code 1 means the array contains errors.
7. Exit code 2 means a usage error or a load error. Read stderr.
8. Show a non-empty array to the user.
9. After you edit a Go file, run the command on that file and read the JSON.

`go run` compiles the linter on each run. You can build a binary and call that binary from `tools/lint/hook.sh`. Do not commit the binary.

The scripts below need `jq` and a POSIX shell. On Windows, run them with Git Bash. Install `jq` when it is missing. When you cannot install `jq`, skip the hook files and run the command yourself.

### tools/lint/hook.sh

Make this file. Make it executable with `chmod +x tools/lint/hook.sh`.

```sh
#!/bin/sh
# Lint Go files. Usage: hook.sh file.go ...
# Exit 0 when every file has no diagnostics.
# Exit 2 when a file has diagnostics or the linter fails.
# Diagnostics and load errors are written to stderr.
set -u
root=$(CDPATH= cd "$(dirname "$0")/../.." && pwd)
status=0
for file in "$@"; do
	case $file in
	*.go) ;;
	*) continue ;;
	esac
	dir=$(CDPATH= cd "$(dirname "$file")" && pwd) || {
		printf 'goxlint: cannot resolve %s\n' "$file" >&2
		status=2
		continue
	}
	abs=$dir/$(basename "$file")
	err=$(mktemp)
	json=$(cd "$root" && go run ./tools/lint --stdin-filename "$abs" --format json < "$abs" 2>"$err")
	code=$?
	if [ "$code" -ne 0 ] || printf '%s' "$json" | grep -q '"rule"'; then
		status=2
		printf 'goxlint: %s\n' "$abs" >&2
		printf '%s\n' "$json" >&2
		cat "$err" >&2
	fi
	rm -f "$err"
done
exit "$status"
```

### Choose the agent

1. Add the hook for the agent that you are.
2. Ask the user before you add a hook for a different agent.
3. When the settings file exists, add the hook to the existing array. Keep the other hooks.
4. For an agent that is not listed here, add its project hook for an edited file. Call `tools/lint/hook.sh` with the path of each edited `.go` file.

### Claude Code

Claude Code shows stderr to the agent when the hook exits with code 2.

1. Make `.claude/hooks/goxlint.sh`. Make it executable.

```sh
#!/bin/sh
set -u
root=$(CDPATH= cd "$(dirname "$0")/../.." && pwd)
file=$(jq -r '.tool_input.file_path // empty')
if [ -z "$file" ] || [ "$file" = null ]; then
	exit 0
fi
"$root/tools/lint/hook.sh" "$file"
```

2. Add this hook to `.claude/settings.json`:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/goxlint.sh"
          }
        ]
      }
    ]
  }
}
```

### Cursor

Cursor runs a project hook when the workspace is trusted. `afterFileEdit` sends the edited path in `file_path`.

1. Make `.cursor/hooks/goxlint.sh`. Make it executable.

```sh
#!/bin/sh
set -u
root=$(CDPATH= cd "$(dirname "$0")/../.." && pwd)
file=$(jq -r '.file_path // empty')
if [ -z "$file" ] || [ "$file" = null ]; then
	exit 0
fi
"$root/tools/lint/hook.sh" "$file"
```

2. Add this hook to `.cursor/hooks.json`:

```json
{
  "version": 1,
  "hooks": {
    "afterFileEdit": [
      {
        "command": ".cursor/hooks/goxlint.sh"
      }
    ]
  }
}
```

### Codex

Codex does not run a hook until the user trusts it. Tell the user to review the hook with `/hooks`.

A patch lists files with `*** Add File:`, `*** Update File:`, and `*** Move to:`. Do not lint a `*** Delete File:` path.

1. Make `.codex/hooks/goxlint.sh`. Make it executable.

```sh
#!/bin/sh
set -u
root=$(CDPATH= cd "$(dirname "$0")/../.." && pwd)
patch=$(jq -r '.tool_input.command // empty')
if [ -z "$patch" ] || [ "$patch" = null ]; then
	exit 0
fi
list=$(mktemp)
printf '%s\n' "$patch" | awk '
function flush() { if (path != "") print path; path = "" }
/^\*\*\* (Add|Update) File:/ { flush(); path = $0; sub(/^\*\*\* (Add|Update) File:[[:space:]]*/, "", path); next }
/^\*\*\* Move to:/ { path = $0; sub(/^\*\*\* Move to:[[:space:]]*/, "", path); next }
/^\*\*\* / { flush(); next }
END { flush() }
' > "$list"
status=0
while IFS= read -r file; do
	[ -n "$file" ] || continue
	"$root/tools/lint/hook.sh" "$file" || status=2
done < "$list"
rm -f "$list"
exit "$status"
```

2. Add this hook to `.codex/hooks.json`:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "apply_patch",
        "hooks": [
          {
            "type": "command",
            "command": ".codex/hooks/goxlint.sh"
          }
        ]
      }
    ]
  }
}
```

## 5. Run the linter

1. Run `go run ./tools/lint ./...`.
2. Tell the user the exit code.
3. A load error is on stderr and starts with `goxlint: load:`. Show it to the user.
4. With no rules, an empty JSON array is the result of `go run ./tools/lint --format json ./...`.
