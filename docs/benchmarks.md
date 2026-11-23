# Benchmarks

This document tells you how to measure goxlint.

goxlint has two measurements:

1. Engine benchmarks. These are Go benchmarks in this repository. They measure rule time inside the engine.
2. A comparison with other linters. The script `bench/compare.sh` runs that comparison. CI does not run the script.

## Engine benchmarks

The engine benchmarks are in `internal/engine/bench_test.go` and `internal/engine/timing_bench_test.go`.

`go test ./...` does not run them. Go runs a benchmark only when you pass `-bench`.

### What each benchmark measures

`BenchmarkLintFile` calls `LintFile` on one generated file.

- It has one part for syntax mode and one part for typed mode.
- Each part has a rule count of 1, 10, 50, and 200.
- Each rule handles `*ast.CallExpr`, `*ast.Ident`, and `*ast.FuncDecl`.

`BenchmarkLintFileSeparateWalks` uses the same file, the same rule counts, and the same checks. Each rule walks the file with `ast.Inspect`. This is the baseline for one walk per rule. `ast.Inspect` visits every node. The engine walk visits only the three node types above. The check on a visited node is the same function in both benchmarks.

`BenchmarkLintAll` calls `LintAll` on 200 generated files in one package.

- It has one part for syntax mode and one part for typed mode.
- Each part has a file-rule count of 1, 10, 50, and 200.
- Each part also runs one package rule, then the finish pass for every file.
- `LintAll` lints files at the same time, up to the Go processor count.

`BenchmarkLintAllRepository` calls `LintAll` on the `.go` files of one repository. The rule is in the benchmark file. It reports identifiers named `err`. Set `GOXLINT_BENCH_ROOT` to the same directory when you compare two revisions. When the variable is empty, the benchmark uses this repository.

A typed part type-checks its files before the timer starts. The measured time is rule time. The measured time does not include type-checking. The repository benchmark parses its files before the timer starts. Its measured time does not include that parse.

### The generated input

The single-file benchmarks use one file, `large.go`, in package `bench`. The file has 200 function pairs. Each pair is one caller and one helper, so the file has 400 functions.

The package benchmark uses 200 files, `file_000.go` to `file_199.go`, in package `bench`. Each file has 4 function pairs, so each file has 8 functions. The names differ in every file, and the files type-check as one package.

A file rule keeps a node when the remainder of the first byte of the name, divided by 7, equals the remainder of the rule index divided by 7. In typed mode, a kept node also reads type information. The package rule reads the name of each top-level function. In typed mode it also reads the function object.

The benchmarks call `b.ReportAllocs()`.

### Run the benchmarks

1. Open a terminal at the root of this repository.
2. Run this command:

```sh
go test -run '^$' -bench . -benchmem ./internal/engine
```

To repeat a benchmark, add `-count` and `-benchtime`. The results below use `-count=11` and `-benchtime=1s`.

### Compare two revisions of the engine

Use the repository benchmark when you compare two checkouts. Use one snapshot for both checkouts, so a change in the tree does not change the input.

1. Choose a directory of Go files. This directory is the snapshot.
2. Set `GOXLINT_BENCH_ROOT` to that directory.
3. Check out the first revision.
4. Run the command below. It takes 8 samples.

```sh
go test -run '^$' -bench BenchmarkLintAllRepository -benchmem -count=8 -benchtime=1s ./internal/engine
```

5. Check out the second revision.
6. Run the same command with the same `GOXLINT_BENCH_ROOT`.
7. Compare the median of the 8 samples from each revision.
8. Use a two-sample rank test when you need to know whether the samples from the two revisions are consistently different. The rank test answers that question only. Decide separately whether the difference matters for a user.

### Machine for the tables below

| Item | Value |
| --- | --- |
| Date | 2026-11-23, 07:17 UTC |
| OS | Linux 6.12.113+, x86_64 |
| Machine | KVM virtual machine |
| CPU | `Intel(R) Xeon(R) Processor`, family 6, model 207 |
| Processors | 4. Go left `GOMAXPROCS` at the default, so the benchmark names end in `-4`. |
| Memory | 16387328 kB (`MemTotal`) |
| Go | `go1.26.0 linux/amd64` |

The command was:

```sh
go test ./internal/engine -run '^$' -bench 'BenchmarkLint' -benchmem -count=11 -benchtime=1s
```

Each table uses the middle sample of the 11 samples, after sorting by time. The minimum and the maximum are the fastest and slowest of the 11 samples. Bytes and allocations come from that middle sample. The run was on the engine before the use index and the concrete walk filter. The current engine allocates a use index for each file and leaves it unbuilt until a rule calls `UsesOf`. These benchmarks do not call `UsesOf`.

**CAUTION:** These times come from one virtual machine. The host and the guest share the CPU. The minimum and the maximum show the range on that machine. Run the command again on your machine before you use a difference as evidence of an engine change.

### One file

Time is in milliseconds. The value in parentheses is the minimum and the maximum.

#### Syntax mode

| Rules | Single walk (ms) | Separate walks (ms) | Single walk bytes/op | Single walk allocs/op | Separate walks bytes/op | Separate walks allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1.039 (0.980–1.185) | 0.094 (0.093–0.101) | 2174297 | 53 | 24 | 1 |
| 10 | 1.578 (1.492–1.680) | 0.914 (0.908–0.927) | 2180345 | 146 | 240 | 10 |
| 50 | 3.524 (3.445–3.754) | 4.606 (4.579–4.808) | 2205813 | 512 | 1200 | 50 |
| 200 | 11.501 (11.378–12.019) | 18.298 (18.196–18.419) | 2304118 | 1868 | 4800 | 200 |

#### Typed mode

| Rules | Single walk (ms) | Separate walks (ms) | Single walk bytes/op | Single walk allocs/op | Separate walks bytes/op | Separate walks allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1.242 (1.027–1.338) | 0.117 (0.114–0.125) | 2174295 | 53 | 24 | 1 |
| 10 | 1.903 (1.728–2.079) | 1.029 (1.020–1.066) | 2180345 | 146 | 240 | 10 |
| 50 | 4.168 (4.009–4.491) | 5.127 (5.096–5.628) | 2205812 | 512 | 1200 | 50 |
| 200 | 13.235 (13.186–13.754) | 20.490 (20.324–21.774) | 2304118 | 1868 | 4800 | 200 |

On this run, in syntax mode, 200 rules take 11.1 times as long as 1 rule in the single walk. In the separate walks, 200 rules take 195 times as long as 1 rule.

At 1 rule, the single walk takes 11.0 times as long as one separate walk. At 200 rules, the separate walks take 1.6 times as long as the single walk. The change in order is between 10 rules and 50 rules on this input.

Typed mode has the same pattern. At 200 rules, the typed single walk takes 1.2 times as long as the syntax single walk. That extra time is the type lookup inside the rules. The type-check of the file stays outside the timer.

The single walk also calls `Create`, scans suppressions, and sorts the result. The separate walks only walk the file and run the checks. The byte counts show that difference. The single walk allocates about 2.2 MB on every operation. The separate walks allocate 24 bytes for each rule.

### Package of 200 files

The rule count is the number of file rules. Every row also runs one package rule.

| Rules | Syntax median (ms) | Syntax min–max (ms) | Typed median (ms) | Typed min–max (ms) |
| --- | ---: | ---: | ---: | ---: |
| 1 | 2.052 | 2.009–2.468 | 2.392 | 2.349–2.413 |
| 10 | 3.701 | 3.576–3.932 | 4.288 | 4.215–4.413 |
| 50 | 11.148 | 10.393–15.271 | 12.003 | 11.777–12.990 |
| 200 | 36.879 | 35.303–45.131 | 38.882 | 38.651–40.738 |

On this run, in syntax mode, 200 file rules take 18.0 times as long as 1 file rule. In typed mode, the ratio is 16.3. The time grows more slowly than the rule count. The syntax row for 50 rules has the widest spread on this machine (10.393 ms to 15.271 ms).

| Rules | Syntax bytes/op | Syntax allocs/op | Typed bytes/op | Typed allocs/op |
| --- | ---: | ---: | ---: | ---: |
| 1 | 4075787 | 5849 | 4075791 | 5849 |
| 10 | 5285385 | 24449 | 5285387 | 24449 |
| 50 | 10379775 | 97649 | 10379818 | 97649 |
| 200 | 30040576 | 368849 | 30040576 | 368849 |

### This repository

`BenchmarkLintAllRepository` used this repository, because `GOXLINT_BENCH_ROOT` was empty. The rule reports identifiers named `err`. It is a different rule from the generated-file benchmarks.

| Median (ms) | Minimum (ms) | Maximum (ms) | Bytes/op | Allocs/op |
| ---: | ---: | ---: | ---: | ---: |
| 2.991 | 2.941 | 3.104 | 9156043 | 1914 |

### Limits of the engine benchmarks

- The generated files are regular call patterns. A repository with different files can take a different amount of time.
- The typed rows exclude type-checking. On a real run, typed mode also loads and type-checks dependencies. That load time is outside these tables.
- The separate-walk rows are a baseline in the same process. They are not a run of revive, and they are not a run of the goxlint command.
- The repository row excludes the parse. It uses one syntax rule.
- The tables are one run on the virtual machine above.

## Comparison with other linters

`bench/compare.sh` measures wall time and peak memory for a small set of checks on one real repository. It also records goxlint load time and rule time from `--timing`.

CI does not run this script. The first run needs network access. The script clones a repository, installs two tools, and builds `examples/goxlint`.

### What the script runs

The target is `prometheus/prometheus` at tag `v3.16.0`, commit `e2e28adeee2c5883d000fc82aca6008cfbf7e96d`. The script checks the commit after the clone. The clone is `bench/src/prometheus`. Git ignores that directory.

The script builds `examples/goxlint` from the current checkout. It installs these tools into `bench/bin`:

| Tool | Module | Version |
| --- | --- | --- |
| golangci-lint | `github.com/golangci/golangci-lint/v2/cmd/golangci-lint` | `v2.15.0` |
| revive | `github.com/mgechev/revive` | `v1.17.1` |

`go install` uses a version suffix, so it does not change `go.mod`. The script stops when `go.mod` or `go.sum` changes.

Each case uses the pattern `./...`, unless you set `BENCH_PATTERN`. The script warms the Go build cache with `go list -deps -export` before the measured runs. Each case then has warmup runs (default 1) and measured runs (default 5). Set `BENCH_WARMUP` and `BENCH_RUNS` to change those counts.

| Case | Command | Config |
| --- | --- | --- |
| `goxlint-load` | example goxlint, no rules, `--timing` | `bench/goxlint-none.json` |
| `go-list` | `go list -deps -export` | none |
| `goxlint-syntax` | example goxlint, syntax rules, `--timing` | `bench/goxlint-syntax.json` |
| `revive-syntax` | revive, five syntax rules | `bench/revive.toml` |
| `goxlint-typed` | example goxlint, typed rules, `--timing` | `bench/goxlint-typed.json` |
| `golangci-typed` | golangci-lint, four typed linters | `bench/golangci.yml` |

`goxlint-load` enables no rules, so goxlint stays in syntax mode. Its `--timing` load row is parse time. `go-list` is the load work that typed mode pays. Compare that row with the load row of `goxlint-typed`, which is `Load (typed mode)`.

The syntax rules in the example linter are:

- `anti-slop/no-generic-package-names`
- `anti-slop/no-panic-in-library`
- `anti-slop/no-placeholder-comments`
- `anti-slop/no-restating-comments`
- `anti-slop/require-safety-comment-for-type-assertion`

The revive rules are `blank-imports`, `context-as-argument`, `error-return`, `error-strings`, and `unreachable-code`.

The typed rules in the example linter are:

- `anti-slop/no-blank-error`
- `anti-slop/no-detached-context`
- `anti-slop/no-error-string-matching`
- `anti-slop/no-swallowed-error`

The golangci-lint linters are `errcheck`, `govet`, `ineffassign`, and `unused`.

Each set has a similar number of checks. The checks differ. A shorter time means that this set finished sooner on this target.

The configs set findings to warnings and set the process exit code for findings to 0. `reportUnusedSuppressions` is `off` in the goxlint configs. The script stops when a command exits with a non-zero status.

### Requirements

- Go 1.26 or later.
- Bash.
- `git`.
- `/usr/bin/time`. On Linux, install the `time` package. The shell builtin `time` does not report peak memory. On macOS, `/usr/bin/time` is part of the system.
- Network access on the first run, to clone Prometheus and to install the tools.
- `hyperfine`, when you want it to repeat the runs. The script runs the repeats itself when `hyperfine` is absent.

The script runs on Linux and macOS.

### Run the comparison

1. Open a terminal at the root of this repository.
2. Run:

```sh
bash bench/compare.sh
```

3. Read `bench/out/results.txt`.

The file starts with the machine, the OS, the processor count, the Go version, the tool versions, and the target commit. Then it prints one row for each case:

| Column | Meaning |
| --- | --- |
| `WALL_S_MED` | Median wall time, in seconds. |
| `WALL_S_MIN` | Fastest measured run, in seconds. |
| `WALL_S_MAX` | Slowest measured run, in seconds. |
| `RSS_MIB_MED` | Median of the per-run peak resident set size, in MiB. |
| `RSS_MIB_MAX` | Highest per-run peak resident set size, in MiB. |
| `LOAD_MS` | Median goxlint load time from `--timing`, in milliseconds. `n/a` when the tool does not print that table. |
| `RULE_MS` | Median sum of the goxlint rule rows, in milliseconds. `n/a` for the other tools. |

Wall time comes from `/usr/bin/time`. On Linux that value has a resolution of 0.01 seconds. When `hyperfine` is installed, `hyperfine` repeats the runs, and the script drops the warmup samples before it takes the median. The median is still the `/usr/bin/time` median.

The script sends stdout to `/dev/null`. The tools still build that output. Stderr is kept for the goxlint timing table.

Run `bash bench/compare.sh --self-test` to check the median, the timing parser, and `/usr/bin/time` without a clone. That check runs `/bin/true`. It does not measure a linter.

### Comparison results

**CAUTION:** This document has no wall times for Prometheus. The engine tables come from the KVM virtual machine in [Machine for the tables below](#machine-for-the-tables-below). That guest shares its CPU with the host, and the guest memory size can change while it runs. Run `bench/compare.sh` on a machine that you control. Keep the machine block from `bench/out/results.txt` with the numbers. Put comparison times in this section only after that run.

The comparison table is pending that run.

### Limits of the comparison

- The rule sets are small, and the checks differ. Read the lists above before you compare a row.
- `goxlint-load` is syntax mode. Compare package load time with `go-list` and with the load row of `goxlint-typed`.
- `go list -deps -export` skips test files. Typed mode in goxlint loads test files. The two commands measure different work.
- Syntax mode reads every `.go` file. It does not apply build tags. Typed mode and golangci-lint follow the Go build rules.
- revive skips `vendor` when you do not pass an exclude pattern. The other commands use `./...` from the module root, which also stays out of `vendor`.
- The goxlint load row and the goxlint rule rows do not add up to the wall time. Wall time also includes the walk outside handlers, suppression checks, and output.
- Rule time from `--timing` includes the clock calls around each handler. Use it to separate load from rules. Use the wall time to compare programs.
- The script uses a warm build cache. A cold cache takes longer. The cold run is the warmup, and it is not in the median.
- A finding in the target repository does not fail the script. The configs above keep a finding at exit code 0. A usage error or a failed load still fails the script.
