# Configuration

goxlint reads its configuration from a JSON file with the name `.goxlintrc.json`.

## Find the file

goxlint starts in the current directory. It looks for `.goxlintrc.json` in that
directory and then in each parent directory. goxlint uses the first file that it
finds. To use a different file, give the `-c` flag.

Paths in the file are relative to the directory that contains the file. If
there is no file, paths are relative to the current directory.

If goxlint finds no file, it uses this configuration:

```json
{ "categories": { "correctness": "warn" } }
```

## Example

```json
{
  "categories": {
    "correctness": "error",
    "suspicious": "error",
    "style": "warn"
  },
  "rules": {
    "acme/no-println": "error",
    "acme/max-params": ["warn", { "max": 5 }]
  },
  "ignorePatterns": ["gen/**", "**/*.pb.go"],
  "overrides": [
    {
      "files": ["**/*_test.go"],
      "rules": { "acme/no-println": "off" }
    }
  ],
  "reportUnusedSuppressions": "error"
}
```

goxlint does not accept unknown fields, unknown rule IDs, or unknown categories.
If the file has an error, goxlint shows the error and exits with `2`.

## Fields

| Field | Type | Description |
| --- | --- | --- |
| `categories` | object | The severity of each category. |
| `rules` | object | The severity and the options of each rule. |
| `ignorePatterns` | array of strings | Files and directories that goxlint does not lint. |
| `overrides` | array of objects | Rule settings for files that agree with patterns. |
| `reportUnusedSuppressions` | string | The severity of unused-suppression reports. The default is `warn`. |

## Severities

| Value | Alias | Result |
| --- | --- | --- |
| `"off"` | `"allow"` | The rule does not run. |
| `"warn"` | | The rule reports warnings. Warnings do not change the exit code, unless you use `--deny-warnings` or `--max-warnings`. |
| `"error"` | `"deny"` | The rule reports errors. Errors make the exit code `1`. |

## categories

Each rule has one category. A category setting applies to all rules in that
category.

| Category | Use |
| --- | --- |
| `correctness` | Code that is wrong or has no use. |
| `suspicious` | Code that is almost always wrong. |
| `pedantic` | Strict rules. They can report code that is correct. |
| `style` | Code that is not idiomatic or is difficult to read. |
| `restriction` | Patterns that are valid, but that a project does not permit. |
| `nursery` | Rules that are not complete. |

## rules

A `rules` entry sets one rule. The key is the rule ID. A `rules` entry is more
specific than a `categories` entry, so it replaces the category setting for
that rule.

The value has one of two forms:

- A severity: `"error"`.
- An array with a severity and an options object: `["warn", { "max": 5 }]`.

The rule defines its options. Read the documentation of the rule. If the
options are not correct, goxlint reports an `invalid options` diagnostic. A rule
that obeys the API conventions then does not run on the file. For more information, read [Writing rules](writing-rules.md#options).

To see all rule IDs, run `goxlint rules`.

## ignorePatterns

goxlint does not lint files and directories that agree with these patterns.
Package rules do not get ignored files. In typed mode, the Go tool can still
type-check an ignored file as part of its package.

| Pattern | Agrees with |
| --- | --- |
| `gen/**` | The directory `gen` and all files below it. |
| `**/*.pb.go` | All `.pb.go` files at all levels. |
| `*.pb.go` | The same as `**/*.pb.go`. A pattern without `/` agrees with a file name at all levels. |
| `internal/legacy/*.go` | `.go` files directly in `internal/legacy`. |

The pattern syntax:

- Use `/` between path segments, also on Windows. This applies to
  `ignorePatterns` and override `files` patterns. Paths are relative to the
  directory that contains the configuration file.
- `**` agrees with zero or more path segments.
- `*`, `?`, and `[...]` have the same meaning as in Go's `path.Match`.
- A pattern that ends with `/**` also agrees with the directory itself.
- A leading `./` has no effect.

goxlint always skips these items. You do not need to ignore them:

- Generated files. A generated file has a `// Code generated ... DO NOT EDIT.`
  comment.
- In syntax mode, directories with the names `vendor`, `testdata`, and
  `node_modules`, and directories whose names start with `.` or `_`.

## overrides

An override changes rule settings for some files. Each override has two fields:

| Field | Description |
| --- | --- |
| `files` | Patterns. The override applies to a file that agrees with one or more patterns. |
| `rules` | Rule settings, in the same form as the top-level `rules` field. |

goxlint applies all overrides that agree with a file, in the order of the array.
A later override replaces the setting of an earlier override for the same rule.
An override setting replaces the severity and the options of the rule. goxlint
does not merge options.

Overrides do not accept categories. Set each rule in an override.

```json
{
  "overrides": [
    { "files": ["cmd/**"], "rules": { "acme/no-println": "off" } },
    { "files": ["**/*_test.go"], "rules": { "acme/max-params": ["warn", { "max": 8 }] } }
  ]
}
```

## reportUnusedSuppressions

This field sets the severity of the `goxlint/unused-suppression` report. The
accepted values are the severities in the table above. The default is `warn`.
The `--report-unused-suppressions` flag replaces this value for one run.

This setting is not a rule. Do not add `goxlint/unused-suppression` to `rules`.
For more information, read [Suppressions](suppressions.md).

## Typed mode and syntax mode

goxlint selects the load mode from the configuration. If one or more enabled
rules have `NeedsTypes`, goxlint uses typed mode. This includes rules that only an
override enables. If no enabled rule needs types, goxlint uses syntax mode. Syntax
mode is much faster.

The summary line of the `default` format shows the mode. For more information,
read [How goxlint works](how-goxlint-works.md).

## Command-line changes

The `-D`, `-W`, and `-A` flags change the configuration for one run. For more
information, read [Command line](cli.md#configuration).
