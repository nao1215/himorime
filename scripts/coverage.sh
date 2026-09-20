#!/bin/sh
# Combine unit-test coverage with coverage collected from the real CLI E2E
# suite. Go's GOCOVERDIR data is merged so covered statements are counted once
# rather than averaging two percentages. The scratch directory is recreated on
# every run so stale data cannot make a unit-only or partial run look complete.
set -eu

root=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
cd "$root"

cov="$root/.coverage"
rm -rf "$cov"
mkdir -p "$cov/unit" "$cov/e2e" "$cov/bin" "$cov/merged"
rm -f "$root/cover.out" "$root/cover.html"

# Keep the denominator to shipped production code: the CLI package and all
# internal packages. Test runners, helpers and example programs are not part
# of the production coverage contract.
coverpkg=$(go list -f '{{.ImportPath}}' .)
coverpkg="$coverpkg,$(go list -f '{{.ImportPath}}' ./internal/... | tr '\n' ',' | sed 's/,$//')"

echo ">> unit coverage -> $cov/unit"
go test -count=1 -cover -covermode=atomic -coverpkg="$coverpkg" ./... \
	-args -test.gocoverdir="$cov/unit"

echo ">> building covered himorime -> $cov/bin/himorime"
go build -cover -covermode=atomic -coverpkg="$coverpkg" -o "$cov/bin/himorime" .

echo ">> CLI E2E coverage -> $cov/e2e"
HIMORIME_BINARY="$cov/bin/himorime" GOCOVERDIR="$cov/e2e" \
	go run ./test/e2e/run

# A successful E2E runner is not enough: a miswired binary or environment can
# complete every scenario without writing coverage data. Require both metadata
# and counters before publishing a merged profile.
test -n "$(find "$cov/e2e" -type f -name 'covmeta.*' -size +0c -print -quit)" || {
	echo "coverage: E2E produced no coverage metadata" >&2
	exit 1
}
test -n "$(find "$cov/e2e" -type f -name 'covcounters.*' -size +0c -print -quit)" || {
	echo "coverage: E2E produced no coverage counters" >&2
	exit 1
}

# Keep a unit-only text profile for validation before merging. covdata merge
# adds counters, so every unit count must remain covered at least as often and
# at least one production block must be covered only by the real CLI E2E run.
go tool covdata textfmt -i="$cov/unit" -o="$cov/unit.out"
go tool covdata merge -i="$cov/unit,$cov/e2e" -o="$cov/merged"
go tool covdata textfmt -i="$cov/merged" -o="$root/cover.out"

awk '
FNR == NR {
	if ($1 != "mode:") unit[$1] = $3
	next
}
$1 == "mode:" { next }
{
	seen[$1] = 1
	if ($1 in unit && $3 < unit[$1]) {
		printf "coverage: merged count regressed for %s\n", $1 > "/dev/stderr"
		exit 1
	}
	if ($1 ~ /(^|\/)main\.go:/ && $3 > 0) main_covered = 1
	if (($1 in unit && unit[$1] == 0 && $3 > 0) || (!($1 in unit) && $3 > 0)) e2e_only = 1
}
END {
	if (!main_covered) {
		print "coverage: the real main entry point is not covered" > "/dev/stderr"
		exit 1
	}
	for (block in unit) {
		if (!(block in seen)) {
			printf "coverage: merged profile is missing unit block %s\n", block > "/dev/stderr"
			exit 1
		}
	}
	if (!e2e_only) {
		print "coverage: merged profile has no E2E-only covered production block" > "/dev/stderr"
		exit 1
	}
}' "$cov/unit.out" "$root/cover.out"

go tool cover -html="$root/cover.out" -o "$root/cover.html"
go tool cover -func="$root/cover.out" | tail -n 1
echo ">> wrote cover.out and cover.html (unit + CLI E2E combined)"
