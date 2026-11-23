#!/usr/bin/env bash
# Compare the example goxlint linter with go list, revive, and golangci-lint.
# CI does not run this script. It clones a repository and installs tools.
# Results go to bench/out/. The clone and the tools are gitignored.

set -euo pipefail
export LC_ALL=C

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SCRIPT="$SCRIPT_DIR/compare.sh"
REPO=$(cd "$SCRIPT_DIR/.." && pwd)
BIN="$REPO/bench/bin"
SRC="$REPO/bench/src/prometheus"
OUT="$REPO/bench/out"
CONFIG="$REPO/bench"

PROMETHEUS_URL=https://github.com/prometheus/prometheus.git
PROMETHEUS_TAG=v3.16.0
PROMETHEUS_COMMIT=e2e28adeee2c5883d000fc82aca6008cfbf7e96d
GOLANGCI_LINT_VERSION=v2.15.0
GOLANGCI_LINT_PKG=github.com/golangci/golangci-lint/v2/cmd/golangci-lint
REVIVE_VERSION=v1.17.1
REVIVE_PKG=github.com/mgechev/revive

RUNS=${BENCH_RUNS:-5}
WARMUP=${BENCH_WARMUP:-1}
PATTERN=${BENCH_PATTERN:-./...}

TIME_KIND=""

usage() {
	cat <<'EOF'
Usage: bench/compare.sh [--self-test]

Measure load time, syntax rules, and typed rules on a pinned Prometheus commit.
The script writes machine details and a results table to bench/out/results.txt.

Environment:
  BENCH_RUNS      measured runs for each command (default 5)
  BENCH_WARMUP    unmeasured runs before the measured runs (default 1)
  BENCH_PATTERN   package pattern (default ./...)

CI does not run this script. The first run needs network access.
EOF
}

die() {
	echo "compare.sh: $*" >&2
	exit 1
}

cpu_count() {
	if command -v nproc >/dev/null 2>&1; then
		nproc
	else
		sysctl -n hw.ncpu
	fi
}

gnu_time() {
	if [[ -z $TIME_KIND ]]; then
		if [[ ! -x /usr/bin/time ]]; then
			die "/usr/bin/time is not installed"
		fi
		if /usr/bin/time --version >/dev/null 2>&1; then
			TIME_KIND=gnu
		else
			TIME_KIND=bsd
		fi
	fi
	[[ $TIME_KIND == gnu ]]
}

# median reads one number per line and prints the median.
median() {
	sort -n | awk '
		{ a[NR] = $1 + 0 }
		END {
			if (NR == 0) exit 1
			if (NR % 2 == 1) printf "%.6f\n", a[(NR + 1) / 2]
			else printf "%.6f\n", (a[NR / 2] + a[NR / 2 + 1]) / 2
		}
	'
}

min_num() { sort -n | awk 'NR == 1 { m = $1 } END { if (NR == 0) exit 1; print m }'; }
max_num() { sort -n | awk 'NR == 1 { m = $1 } { m = $1 } END { if (NR == 0) exit 1; print m }'; }

# parse_timing reads one goxlint --timing table and prints "load rules" in ms.
# The rule total is the sum of the rows in that table. The table has at most
# 10 rules. The configs in bench/ enable fewer than 10 rules.
parse_timing() {
	awk '
		/Time \(ms\)/ { table = 1 }
		table && $1 == "Load" { load = $NF; next }
		table && $NF ~ /%$/ { rules += $(NF - 1) }
		END {
			if (load == "") exit 1
			printf "%.6f %.6f\n", load, rules + 0
		}
	' "$1"
}

shell_join() {
	local out="" word
	for word in "$@"; do
		out="$out $(printf '%q' "$word")"
	done
	printf '%s' "${out# }"
}

record_main() {
	if [[ "${1:-}" == "--" ]]; then
		shift
	fi
	[[ $# -ge 1 ]] || die "--record needs a command"
	[[ -n ${BENCH_RECORD_DIR:-} ]] || die "BENCH_RECORD_DIR is empty"
	mkdir -p "$BENCH_RECORD_DIR"
	local run err timef code
	run=$(mktemp -d "$BENCH_RECORD_DIR/run.XXXXXX")
	err="$run/err"
	timef="$run/time"
	code=0
	if gnu_time; then
		set +e
		GOXLINT_BENCH_ERR="$err" /usr/bin/time -f '%e %M' -o "$timef" -- \
			sh -c 'exec "$0" "$@" >/dev/null 2>"$GOXLINT_BENCH_ERR"' "$@"
		code=$?
		set -e
	else
		local raw="$run/raw"
		set +e
		GOXLINT_BENCH_ERR="$err" /usr/bin/time -l \
			sh -c 'exec "$0" "$@" >/dev/null 2>"$GOXLINT_BENCH_ERR"' "$@" \
			2>"$raw"
		code=$?
		set -e
		awk '
			/ real / { sec = $1 }
			/maximum resident set size/ { kb = $1 / 1024 }
			END {
				if (sec == "" || kb == "") exit 1
				printf "%.6f %.0f\n", sec, kb
			}
		' "$raw" >"$timef"
	fi
	printf '%s\n' "$code" >"$run/code"
	if [[ ! -s $timef ]]; then
		echo "no timing data for: $*" >&2
		exit 1
	fi
	if [[ $code -ne 0 ]]; then
		echo "command failed with exit $code: $*" >&2
		echo "stderr:" >&2
		cat "$err" >&2 || true
		exit "$code"
	fi
}

drop_oldest_runs() {
	local dir=$1 drop=$2 line n=0
	while IFS= read -r line; do
		[[ -n $line ]] || continue
		n=$((n + 1))
		if [[ $n -le $drop ]]; then
			rm -rf "$line"
		fi
	done < <(ls -trd "$dir"/run.* 2>/dev/null || true)
}

count_runs() {
	local dir=$1 n=0 run
	for run in "$dir"/run.*; do
		[[ -d $run ]] || continue
		n=$((n + 1))
	done
	printf '%s\n' "$n"
}

summarize_case() {
	local name=$1 dir=$2
	local seconds="$OUT/$name.seconds" rss="$OUT/$name.rss"
	local loads="$OUT/$name.load" rules="$OUT/$name.rules"
	local run sec kb parsed code n
	: >"$seconds"
	: >"$rss"
	: >"$loads"
	: >"$rules"
	n=0
	for run in "$dir"/run.*; do
		[[ -d $run ]] || continue
		n=$((n + 1))
		read -r sec kb <"$run/time"
		printf '%s\n' "$sec" >>"$seconds"
		printf '%s\n' "$kb" >>"$rss"
		code=$(cat "$run/code")
		if [[ $code -ne 0 ]]; then
			die "$name has a failed run in $run"
		fi
		if [[ -s $run/err ]] && parse_timing "$run/err" >/dev/null 2>&1; then
			parsed=$(parse_timing "$run/err")
			printf '%s\n' "${parsed%% *}" >>"$loads"
			printf '%s\n' "${parsed##* }" >>"$rules"
		fi
	done
	if [[ $n -ne $RUNS ]]; then
		die "$name produced $n measured runs, want $RUNS"
	fi
	local wall_med wall_min wall_max rss_med rss_max load_cell rule_cell
	wall_med=$(median <"$seconds")
	wall_min=$(min_num <"$seconds")
	wall_max=$(max_num <"$seconds")
	rss_med=$(awk '{ printf "%.6f\n", $1 / 1024 }' "$rss" | median)
	rss_max=$(awk '{ printf "%.6f\n", $1 / 1024 }' "$rss" | max_num)
	if [[ -s $loads ]]; then
		load_cell=$(median <"$loads" | awk '{ printf "%.1f", $1 }')
		rule_cell=$(median <"$rules" | awk '{ printf "%.1f", $1 }')
	else
		load_cell=n/a
		rule_cell=n/a
	fi
	printf '%-18s %10.3f %10.3f %10.3f %12.1f %12.1f %12s %12s\n' \
		"$name" "$wall_med" "$wall_min" "$wall_max" "$rss_med" "$rss_max" \
		"$load_cell" "$rule_cell" | tee -a "$OUT/results.txt"
}

measure_loop() {
	local name=$1
	shift
	local i
	if [[ $WARMUP -gt 0 ]]; then
		for ((i = 0; i < WARMUP; i++)); do
			if ! "$@" >/dev/null 2>"$OUT/$name.warmup.err"; then
				echo "warmup failed for $name" >&2
				cat "$OUT/$name.warmup.err" >&2 || true
				exit 1
			fi
		done
	fi
	for ((i = 0; i < RUNS; i++)); do
		bash "$SCRIPT" --record -- "$@"
	done
}

measure_hyperfine() {
	local name=$1
	shift
	local joined
	joined=$(shell_join "$@")
	hyperfine --warmup "$WARMUP" --runs "$RUNS" \
		--export-json "$OUT/$name.hyperfine.json" \
		"BENCH_RECORD_DIR=$(printf '%q' "$BENCH_RECORD_DIR") bash $(printf '%q' "$SCRIPT") --record -- $joined"
	drop_oldest_runs "$BENCH_RECORD_DIR" "$WARMUP"
}

measure_case() {
	local name=$1
	shift
	local sample_root="$OUT/samples/$name"
	rm -rf "$sample_root"
	mkdir -p "$sample_root"
	export BENCH_RECORD_DIR="$sample_root"
	echo "== $name =="
	if command -v hyperfine >/dev/null 2>&1; then
		measure_hyperfine "$name" "$@"
	else
		measure_loop "$name" "$@"
	fi
	summarize_case "$name" "$sample_root"
}

version_matches() {
	local bin=$1 version=$2 needle
	shift 2
	[[ -x $bin ]] || return 1
	needle=${version#v}
	"$bin" "$@" 2>&1 | awk -v version="$version" -v needle="$needle" '
		index($0, version) || index($0, needle) { found = 1 }
		END { exit !found }
	'
}

install_tool() {
	local bin=$1 version=$2 pkg=$3
	shift 3
	if version_matches "$bin" "$version" "$@"; then
		return
	fi
	echo "Installing $pkg@$version"
	(cd /tmp && GOBIN="$BIN" go install "$pkg@$version")
	version_matches "$bin" "$version" "$@" || die "$bin does not report $version"
}

install_tools() {
	mkdir -p "$BIN" "$OUT"
	echo "Building examples/goxlint"
	(cd "$REPO" && go build -o "$BIN/goxlint" ./examples/goxlint)
	install_tool "$BIN/golangci-lint" "$GOLANGCI_LINT_VERSION" "$GOLANGCI_LINT_PKG" version
	install_tool "$BIN/revive" "$REVIVE_VERSION" "$REVIVE_PKG" -version
	if ! git -C "$REPO" diff --exit-code -- go.mod go.sum >/dev/null; then
		die "installing tools changed go.mod or go.sum"
	fi
}

clone_target() {
	mkdir -p "$REPO/bench/src"
	if [[ -d $SRC/.git ]]; then
		local got
		got=$(git -C "$SRC" rev-parse HEAD)
		if [[ $got != "$PROMETHEUS_COMMIT" ]]; then
			die "bench/src/prometheus is $got, want $PROMETHEUS_COMMIT. Remove that directory and run again."
		fi
		return
	fi
	echo "Cloning $PROMETHEUS_URL $PROMETHEUS_TAG"
	GIT_TERMINAL_PROMPT=0 git clone --depth 1 --branch "$PROMETHEUS_TAG" "$PROMETHEUS_URL" "$SRC"
	local got
	got=$(git -C "$SRC" rev-parse HEAD)
	if [[ $got != "$PROMETHEUS_COMMIT" ]]; then
		die "cloned $got, want pinned commit $PROMETHEUS_COMMIT"
	fi
}

print_machine() {
	{
		echo "Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
		echo "OS: $(uname -srm)"
		echo "CPUs: $(cpu_count)"
		if [[ -r /proc/cpuinfo ]]; then
			grep -m1 'model name' /proc/cpuinfo || true
			grep -m1 'cpu family' /proc/cpuinfo || true
			grep -m1 $'^model\t' /proc/cpuinfo || true
		elif command -v sysctl >/dev/null 2>&1; then
			sysctl -n machdep.cpu.brand_string || true
		fi
		if [[ -r /proc/meminfo ]]; then
			grep MemTotal /proc/meminfo
		fi
		echo "Go: $(go version)"
		echo "goxlint commit: $(git -C "$REPO" describe --tags --always --dirty) $(git -C "$REPO" rev-parse HEAD)"
		echo "golangci-lint: $("$BIN/golangci-lint" version 2>&1 | head -n 1)"
		echo "revive: $("$BIN/revive" -version 2>&1 | head -n 1)"
		echo "target: prometheus/prometheus $PROMETHEUS_TAG $PROMETHEUS_COMMIT"
		if command -v hyperfine >/dev/null 2>&1; then
			echo "hyperfine: $(hyperfine --version 2>&1 | head -n 1)"
		else
			echo "hyperfine: not installed (the script repeated the runs itself)"
		fi
		echo "runs: $RUNS"
		echo "warmup: $WARMUP"
		echo "pattern: $PATTERN"
		echo
		printf '%-18s %10s %10s %10s %12s %12s %12s %12s\n' \
			CASE WALL_S_MED WALL_S_MIN WALL_S_MAX RSS_MIB_MED RSS_MIB_MAX LOAD_MS RULE_MS
	} | tee "$OUT/results.txt"
}

run_all() {
	install_tools
	clone_target
	print_machine
	echo "Warming the Go build cache"
	# shellcheck disable=SC2086
	(cd "$SRC" && go list -deps -export $PATTERN >/dev/null)
	cd "$SRC"
	# shellcheck disable=SC2086
	measure_case goxlint-load "$BIN/goxlint" --timing --format unix -c "$CONFIG/goxlint-none.json" $PATTERN
	# shellcheck disable=SC2086
	measure_case go-list go list -deps -export $PATTERN
	# shellcheck disable=SC2086
	measure_case goxlint-syntax "$BIN/goxlint" --timing --format unix -c "$CONFIG/goxlint-syntax.json" $PATTERN
	# shellcheck disable=SC2086
	measure_case revive-syntax "$BIN/revive" -config "$CONFIG/revive.toml" -formatter default $PATTERN
	# shellcheck disable=SC2086
	measure_case goxlint-typed "$BIN/goxlint" --timing --format unix -c "$CONFIG/goxlint-typed.json" $PATTERN
	# shellcheck disable=SC2086
	measure_case golangci-typed "$BIN/golangci-lint" run --color never -c "$CONFIG/golangci.yml" $PATTERN
	echo
	echo "Wrote $OUT/results.txt"
}

self_test() {
	local tmp got parsed
	tmp=$(mktemp -d)
	got=$(printf '1\n3\n2\n' | median)
	[[ $got == 2.000000 ]] || die "median of 1,3,2 is $got"
	got=$(printf '1\n2\n3\n4\n' | median)
	[[ $got == 2.500000 ]] || die "median of 1,2,3,4 is $got"

	cat >"$tmp/timing.txt" <<'EOF'
Rule                          Time (ms)   Relative
anti-slop/no-blank-error      12.5        40.0%
anti-slop/no-swallowed-error  18.0        60.0%
Load (typed mode)             2310.5
EOF
	parsed=$(parse_timing "$tmp/timing.txt")
	[[ $parsed == "2310.500000 30.500000" ]] || die "timing parse: $parsed"

	export BENCH_RECORD_DIR="$tmp/loop"
	mkdir -p "$BENCH_RECORD_DIR"
	bash "$SCRIPT" --record -- /bin/true
	local run
	run=$(echo "$BENCH_RECORD_DIR"/run.*)
	[[ -d $run ]] || die "record did not create a run directory"
	read -r sec kb <"$run/time"
	awk -v sec="$sec" -v kb="$kb" 'BEGIN {
		if (sec !~ /^[0-9]+(\.[0-9]+)?$/) exit 1
		if (kb !~ /^[0-9]+$/ || kb + 0 <= 0) exit 1
	}' || die "bad time file: $sec $kb"
	[[ $(cat "$run/code") == 0 ]] || die "true exited $(cat "$run/code")"

	if command -v hyperfine >/dev/null 2>&1; then
		OUT="$tmp/hf"
		RUNS=2
		WARMUP=1
		mkdir -p "$OUT"
		measure_case hf-true /bin/true
		[[ $(count_runs "$OUT/samples/hf-true") == 2 ]] || die "hyperfine kept $(count_runs "$OUT/samples/hf-true") runs"
	fi
	echo "self-test passed"
}

main() {
	case "${1:-}" in
	--record)
		shift
		record_main "$@"
		;;
	--self-test)
		self_test
		;;
	-h | --help | help)
		usage
		;;
	"")
		run_all
		;;
	*)
		usage
		exit 2
		;;
	esac
}

main "$@"
