# Command line

Your linter gets its command line from `cli.Main`. This document uses the name
`goxlint` for the linter command. Use the name of your command.

## Commands

| Command | Action |
| --- | --- |
| `goxlint [flags] [patterns]` | Lint the packages or files that the patterns select. |
| `goxlint rules [flags]` | Show a table or JSON array of registered rules. |
| `goxlint init` | Write `.goxlintrc.json` in the current directory. |
| `goxlint -h` | Show the usage text. |

### Lint

```sh
goxlint ./...
goxlint ./internal/... ./cmd/server
goxlint main.go handler.go
```

If you do not give a pattern or `--stdin-filename`, goxlint uses `./...`. You
can put flags before or after the patterns.

| Pattern | Selects |
| --- | --- |
| `./...` | All packages in the current directory and below it. |
| `dir/...` | All packages in `dir` and below it. |
| `dir` | The package in `dir` only. |
| `file.go` | One file. In typed mode, goxlint loads the package that contains the file. |

### Source from stdin

Use `--stdin-filename` to lint an unsaved Go buffer:

```sh
cat handler.go | goxlint --stdin-filename internal/api/handler.go
cat handler.go | goxlint --stdin-filename internal/api/handler.go --format json
```

The filename can be relative to the working directory or absolute. The source
from stdin replaces the contents on disk for this run. The file does not need
to exist. Do not give package or file patterns with this flag. This is a usage
error with exit code `2`.

Configuration discovery starts in the working directory, as for a normal run.
Use `-c` to select another configuration file. The supplied filename selects
file overrides, ignore patterns, and `_test.go` behavior. An ignored filename
produces no diagnostics and exits with `0`. Generated-file checks and
suppression comments use the source from stdin.

In syntax mode, goxlint parses the buffer and the Go files beside it. In typed
mode, it loads the file's package with the buffer as an overlay. Typed mode
uses the normal module and build-tag rules. Package rules see the buffer and
the other loaded files in its package. Ordinary lint findings are shown and
counted only for the supplied file. Internal rule errors in supporting files
are also shown and cause exit code `1`, even with `--quiet`. Package load
errors still go to stderr and cause exit code `2`.

The summary counts only the supplied file if it was loaded. It does not count
supporting files. An ignored or skipped buffer counts as zero files.

Without a fix flag, diagnostics go to stdout in the selected format. With
`--fix`, `--fix-suggestions`, or `--fix-dangerously`, **stdout contains only the
resulting source**. Diagnostics, the summary, fix messages, and timing go to
stderr. The selected `--format` applies to those diagnostics. Stderr can also
contain operational messages, so it is not a single JSON or SARIF document.
The command does not write the buffer or other package files to disk.

```sh
cat handler.go | goxlint --stdin-filename internal/api/handler.go --fix > fixed.go
```

Buffer fixes use the normal pass limit, syntax checks, and formatting policy.
If no fixes apply, stdout contains the original buffer. This also applies to
ignored buffers. After a rejected fix, stdout contains the last accepted
buffer. Check the exit code before you use the output. A failure to read stdin
exits with `2` and produces no source output.

### rules

`goxlint rules` shows one line for each rule:

| Column | Contents |
| --- | --- |
| `RULE` | The rule ID. |
| `CATEGORY` | The category of the rule. |
| `SCOPE` | `file`, `package`, or `file+package`. |
| `FIX` | The most dangerous fix level of the rule. Empty when the rule has no fixes. |
| `TYPES` | `yes` when the rule needs type information. |
| `DESCRIPTION` | The description from the rule metadata. |

Use `--format json` to show the rules as a JSON array. The array has one object
for each rule. The objects are in rule ID order.

The `enabled` value is the severity of the rule in the configuration: `off`,
`warn`, or `error`. goxlint uses the nearest `.goxlintrc.json`. If there is no
file, goxlint uses the default configuration. Use `-c path` to choose a
configuration file. This flag works only with JSON output. The value does not
include overrides, because an override applies only to the files that it
selects.

Each object includes `id`, `plugin`, `name`, `description`, `category`, `scope`,
`fixable`, `needsTypes`, `messages`, and `enabled`:

```sh
goxlint rules --format json
goxlint rules --format json -c ./config/.goxlintrc.json
```

### init

`goxlint init` writes `.goxlintrc.json` in the current directory. The file enables
the `correctness` category with `warn`, and sets each registered rule to
`error`. If the file exists, the command does not change it and exits with `2`.

## Flags

### Configuration

| Flag | Value | Action |
| --- | --- | --- |
| `--stdin-filename` | path | Read a Go buffer from stdin at this filename. Do not give patterns. |
| `-c` | path | Use this configuration file. If you do not use `-c`, goxlint finds the nearest `.goxlintrc.json`. |
| `-D` | rule or category | Set the severity to `error`. You can use the flag more than one time. |
| `-W` | rule or category | Set the severity to `warn`. You can use the flag more than one time. |
| `-A` | rule or category | Set the severity to `off`. You can use the flag more than one time. |
| `--report-unused-suppressions` | `off`, `warn`, `error` | Set the severity of unused-suppression reports. |
| `--tags` | list | Use these build tags in typed mode, for example `--tags integration,e2e`. |

The `-D`, `-W`, and `-A` flags apply in the order that you give them. They
change the configuration before goxlint resolves it. A later flag for the same
rule or category replaces an earlier flag. A rule setting is more specific than
a category setting. Thus `-A style -D acme/no-foo` enables `acme/no-foo` also
when it is a style rule.

These flags do not change the options of a rule. File overrides in the
configuration still apply after the flags.

### Fixes

| Flag | Applies |
| --- | --- |
| `--fix` | Safe fixes. |
| `--fix-suggestions` | Safe fixes and suggestion fixes. |
| `--fix-dangerously` | All fixes, also dangerous fixes. |

For more information, read [Fixes](fixes.md).

### Output and exit status

| Flag | Value | Action |
| --- | --- | --- |
| `--format` | `default`, `unix`, `json`, `github`, `sarif` | Select the output format. The default is `default`. |
| `--timing` | | Show the 10 slowest rules and load time on stderr. |
| `--quiet` | | Do not show warnings. goxlint still counts them. |
| `--deny-warnings` | | Exit with `1` when there are warnings. |
| `--max-warnings` | number | Exit with `1` when the number of warnings is more than this number. |

## Rule timing

Use `--timing` to find expensive rules:

```sh
goxlint --timing ./...
goxlint --format json --timing ./... > results.json
```

After the normal output, goxlint writes a table to stderr. It shows up to 10
rules, in order from most time to least time. Rules with equal times are in
rule ID order. Load time is on a separate row:

```text
Rule                          Time (ms)   Relative
acme/no-duplicate-functions    412.3       61.2%
acme/no-println                98.1        14.6%
Load (typed mode)             2310.0
```

A rule's time includes its `Create` calls, entry and exit handlers, and
`AnalyzePackage` calls. This includes calls that panic. Rules that do not run
have no row. `--quiet` and suppressions do not remove rule time from the table.

Times are elapsed durations summed across goroutines. They include time spent
waiting and are not CPU measurements. The sum can exceed the run's wall time.
The `Relative` column divides a rule's time by the time of all rules that ran,
including rules below the first 10. Load time is not in this percentage.

With a fix flag, the table includes all lint passes and all loads, including
the final run after formatting. Rule times do not include loading, the shared
AST walk outside handlers, suppression checks, output, or fix processing.
Timing adds measurement cost only when enabled. Use it to compare rules, not
to measure their exact CPU cost. Each timed handler call reads the clock twice.
This cost can make a rule with many small handlers appear more expensive.

The flag does not change stdout or exit codes. JSON and SARIF output stay
valid. Without `--timing`, goxlint does not collect rule times.

## Output formats

Diagnostic file paths use forward slashes (`/`) on every operating system,
including Windows. Paths within the working directory are relative to it.
Other paths stay absolute, except in SARIF output. A directory named
`..foo` inside the working directory also uses a relative path.

### default

The `default` format shows a code frame for each diagnostic. Then it shows a
summary:

```text
  ⚠ acme(no-foo): Do not use `foo`.
   ╭─[api/client.go:12:6]
11 │
12 │ func foo() {}
   ·      ───
13 │
   ╰────
  help: Rename the function.

Found 1 warning and 0 errors.
Finished in 85ms on 12 files with 4 rules using 8 threads (syntax mode).
```

An error has the `×` symbol. A warning has the `⚠` symbol. goxlint uses colors
only when the output is a terminal and the `NO_COLOR` environment variable is
not set.

### unix

One line for each diagnostic. Many editors can read this format.

```text
api/client.go:12:6: Do not use `foo`. [Warning/acme/no-foo]
```

### json

One JSON array. Use this format for scripts and coding agents.

```json
[
  {
    "rule": "acme/no-foo",
    "severity": "warning",
    "file": "api/client.go",
    "line": 12,
    "column": 6,
    "endLine": 12,
    "endColumn": 9,
    "message": "Do not use `foo`.",
    "help": "Rename the function.",
    "fix": "safe"
  }
]
```

The `severity` field is `warning` or `error`. The `help` and `fix` fields are
not present when they are empty. The `fix` field gives the fix level.

### github

GitHub Actions workflow commands. GitHub shows each diagnostic as an annotation
on the pull request.

```text
::warning file=api/client.go,line=12,endLine=12,col=6,endColumn=9,title=acme/no-foo::Do not use `foo`.
```

### sarif

One [SARIF 2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/os/sarif-v2.1.0-os.html)
JSON log. Use this format with code scanning tools.

```sh
goxlint --format sarif ./... > results.sarif
```

The log contains one run. Its tool name is `goxlint`. The rule list contains
one entry for each rule enabled in at least one selected file, after flags
and file overrides. Each entry has the rule ID, description, and category.
An empty description is omitted. The list also contains
`goxlint/unused-suppression` when that check is enabled.

Each diagnostic becomes a result with a rule ID, severity, message, and
source range. Diagnostic help is in `properties.help`. A run with no visible
diagnostics has an empty `results` array. Exit codes and `--quiet` work as
with the other formats.

SARIF file URIs are relative to the working directory, including `../` for
files outside it. A path on another Windows volume uses an absolute `file:`
URI. URIs use `/` and escape special characters, for example a space becomes
`%20`.

Line and column numbers start at 1. The end position is exclusive. SARIF
columns count Unicode code points. Other formats count bytes. Tabs count as
one column in SARIF. All formats use physical positions and ignore `//line`
directives.

## Exit codes

| Code | Condition |
| --- | --- |
| `0` | No errors. The warning limits are not exceeded. |
| `1` | One or more errors, or a warning limit is exceeded. |
| `2` | A usage error, a configuration error, a load error, or a fix that produces invalid Go syntax. |

A load error occurs when a file cannot be read or parsed, or when a package
has type errors. goxlint shows each load error on stderr with the prefix
`goxlint: load:`. goxlint still lints the packages that loaded correctly, and it
shows their diagnostics. For more information, read
[How goxlint works](how-goxlint-works.md#load-errors).

## Use in CI

This GitHub Actions step shows diagnostics as annotations. The step fails when
there are errors.

```yaml
- name: Lint
  run: go run ./tools/lint --format github ./...
```

To make warnings fail the step, add `--deny-warnings`.

### Upload SARIF to GitHub code scanning

1. Put your linter in `./tools/lint`, or change the build path below.
2. Add this workflow to `.github/workflows/lint.yml`.
3. Run the workflow. Read the results in the repository's Security tab.

```yaml
name: Lint
on: [push, pull_request]
jobs:
  lint:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      actions: read
      security-events: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Build linter
        run: go build -o "$RUNNER_TEMP/goxlint" ./tools/lint
      - name: Lint
        id: lint
        run: '"$RUNNER_TEMP/goxlint" --format sarif ./... > results.sarif'
      - name: Upload results
        if: ${{ !cancelled() && steps.lint.outcome != 'skipped' }}
        uses: github/codeql-action/upload-sarif@v4
        with:
          sarif_file: results.sarif
          category: goxlint
```

The upload step runs even when diagnostics make the lint step fail. The job
still fails in that case. Add `--deny-warnings` to make warnings fail the job.
The repository must have code scanning available. For access requirements,
read [Uploading a SARIF file to GitHub](https://docs.github.com/en/code-security/how-tos/find-and-fix-code-vulnerabilities/integrate-with-existing-tools/upload-sarif-file).
