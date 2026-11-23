---
name: goxlint-write-rule
description: "Write and test a goxlint rule from Go examples and counterexamples. Use when the user wants a project convention enforced in Go, or asks for a new rule, a plugin check, or goxlinttest cases."
license: MIT
compatibility: "Requires Go 1.26 or later and a goxlint linter command in the repository."
---

# Write a goxlint rule

A spec is the examples and the counterexamples for one rule. An example is Go code that the rule must report. A counterexample is Go code that the rule must not report.

Write the spec first. The spec stays in the repository as the test file. A later change that breaks the spec fails the test.

When the repository has no linter command, use the `goxlint-setup` skill first.

Read these documents before you write the rule:

- https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/writing-rules.md
- https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/testing-rules.md
- https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/package-rules.md
- https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/fixes.md
- https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/suppressions.md
- https://raw.githubusercontent.com/egoisutolabs/goxlint/main/docs/rule-from-a-spec.md

Follow the shape of the example in `rule-from-a-spec.md`. Use only the API in these documents.

## 1. Get the spec

1. Ask the user for Go code that the rule must report. Each sample is an example.
2. Ask the user for Go code that the rule must not report. Each sample is a counterexample.
3. When the user describes the rule in words, write 3 to 5 examples and 3 to 5 counterexamples.
4. Include a counterexample for each nearby form that must stay valid. A different name, a test file, and a renamed import are common forms.
5. Show the spec to the user.
6. Stop until the user confirms the spec.
7. Do not write the rule before the user confirms the spec.

Each sample must type-check. `goxlinttest` permits unused variables and unused imports. A sample can import the standard library.

## 2. Write the tests first

1. Put each example in an `Invalid` case.
2. Put each counterexample in a `Valid` case.
3. Use `goxlinttest.Run` for one file. Use `goxlinttest.RunPackage` when a case needs more than one file.
4. Set `MessageID` on each error. Set `Line` and `Column` on at least one error.
5. Add one `Valid` case with a suppression. Put `//goxlint:ignore test/rule` and a reason on the line before the report. In a test, the rule ID is `test/rule`.
6. When the user wants a fix, put the corrected source in `Output` on an `Invalid` case. Read the fixes section of the testing document. When the user does not want a fix, leave `Output` empty.
7. Declare the rule with its metadata and a `Create` function that returns nil. For a package rule, declare `AnalyzePackage` and do not report from it yet.
8. Run `go test` on the rule package.
9. Confirm that each `Invalid` case fails because the rule reports nothing.
10. Confirm that each `Valid` case passes.
11. When a case fails to parse or fails to type-check, correct the sample. Keep the behavior that the user confirmed.
12. Do not change the spec to make a test pass.

## 3. Write the rule

Choose these values from the spec. Write them in `goxlint.Meta` before the handler.

1. Set `Name` to lowercase words with hyphens.
2. Read the category list in the writing-rules document. Use `goxlint.Restriction` for a project policy. Use `goxlint.Correctness` only when the code is wrong.
3. Add one message ID for each distinct problem. The template says what is wrong. Put the correction in `Help`.
4. Choose the smallest concrete `go/ast` type that contains the problem. An interface type such as `ast.Expr` visits every node of the file.
5. Set `NeedsTypes` to true when the rule calls `IsPkgFunc`, `CalleeFunc`, `TypeOf`, or `ObjectOf`. Set it to true when the rule must see every file of the package. Leave it false when the syntax tree is sufficient.

### Express the check

This is the only place that decides how the rule matches code. The tests call the rule. They do not call the decision function. Put the decision in one function. `Create` only calls that function. A later change can replace the function. The tests stay the same.

1. When the check looks at one node in one file, return `goxlint.Visit` with `goxlint.On` for that node type.
2. When the check needs the state of one file, add `goxlint.OnExit` for the node that opens the scope. Read the visitor section of the writing-rules document.
3. When the check needs more than one file, use `AnalyzePackage`. Report through the context of the file that contains the primary node. Read the package-rules document.
4. When you attach a fix, set `Meta.Fixable` to the most dangerous level of that fix. Read the fixes document. Add `Output` on the invalid case when it is not set yet.
5. Register the rule on the plugin.
6. When `.goxlintrc.json` does not exist, run `go run ./tools/lint init`.
7. When the file exists, add `"<plugin>/<name>": "error"` to `rules`. `init` does not change an existing file. A rule stays off when its category is off and the file has no entry for that rule.

## 4. Loop until the tests pass

1. Run `go test -race` on the rule package.
2. Change the rule until the tests pass.
3. Do not change a spec case to make a test pass.
4. When a case seems wrong, ask the user. Stop until the user answers.
5. Run the tests again after each change to the rule.

## 5. Run the rule on the repository

1. Run the linter on the repository. From the module root, the command is `go run ./tools/lint ./...`.
2. Show the output to the user.
3. Ask the user to mark each diagnostic as correct or wrong.
4. Add each wrong diagnostic to the spec as a `Valid` case. When many diagnostics have the same shape, add one `Valid` case for that shape. List the files that you did not copy.
5. Go back to step 4.
6. Stop when the user accepts the diagnostics, or when the linter reports none.

## Rules

- Report on the smallest node that shows the problem.
- Keep state in `Create`. goxlint can lint files at the same time. Do not store rule state in a package-level variable.
- Do not change the AST, the source, or the type information.
- A rule must not stop the linter. Do not panic.
- Quote a short fragment with `ctx.Snippet` when the message needs source text.
- Run `gofmt` on the rule file and the test file.
