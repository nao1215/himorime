| Benchmark | Metric | Base | Head | Difference | Change | Interval | Confidence | Tolerance | Result | Reason | Rule | P(regression) | P(improvement) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| version / himorime | Latency | 10.00ms | 5.00ms | -5.00ms | -50.0% | -55.0% … -45.0% | 0.0% | +10% | IMPROVED | - | R1 | - | - |
| version / himorime | CPU time | 12.00ms | 12.10ms | +100.00µs | +0.8% | -2.0% … +4.0% | - | +10% | PASS | the difference is smaller than min\_difference | R1 | - | - |
| version / himorime | Peak RSS | 32.00MiB | 33.00MiB | +1.00MiB | +3.1% | -2.0% … +4.0% | - | +10% | PASS | the difference is smaller than min\_difference | R2 | - | - |
| validate / himorime | Latency | 10.00ms | 10.10ms | +100.00µs | +1.0% | -2.0% … +4.0% | - | +10% | PASS | the difference is smaller than min\_difference | R1 | - | - |
| validate / himorime | CPU time | 12.00ms | 12.10ms | +100.00µs | +0.8% | -2.0% … +4.0% | - | +10% | PASS | the difference is smaller than min\_difference | R1 | - | - |
| validate / himorime | Peak RSS | 32.00MiB | 33.00MiB | +1.00MiB | +3.1% | -2.0% … +4.0% | - | +10% | PASS | the difference is smaller than min\_difference | R2 | - | - |

| Benchmark | Side | Metric | Median | Mean | Stddev | Min | Max | P90 | P95 | P99 | CV | Robust CV | Samples | Status | Reason | Collection |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| version / himorime | base | Latency | 10.00ms | 10.00ms | 0ns | 10.00ms | 10.00ms | 10.00ms | 10.00ms | 10.00ms | 0.000 | 0.000 | 20 | measured | - | C1 |
| version / himorime | base | CPU time | 12.00ms | 12.00ms | 0ns | 12.00ms | 12.00ms | 12.00ms | 12.00ms | 12.00ms | 0.000 | 0.000 | 20 | measured | - | C2 |
| version / himorime | base | Peak RSS | 32.00MiB | 32.00MiB | 0B | 32.00MiB | 32.00MiB | 32.00MiB | 32.00MiB | 32.00MiB | 0.000 | 0.000 | 20 | measured | - | C3 |
| version / himorime | head | Latency | 5.00ms | 5.00ms | 0ns | 5.00ms | 5.00ms | 5.00ms | 5.00ms | 5.00ms | 0.000 | 0.000 | 20 | measured | - | C1 |
| version / himorime | head | CPU time | 12.10ms | 12.10ms | 0ns | 12.10ms | 12.10ms | 12.10ms | 12.10ms | 12.10ms | 0.000 | 0.000 | 20 | measured | - | C2 |
| version / himorime | head | Peak RSS | 33.00MiB | 33.00MiB | 0B | 33.00MiB | 33.00MiB | 33.00MiB | 33.00MiB | 33.00MiB | 0.000 | 0.000 | 20 | measured | - | C3 |
| validate / himorime | base | Latency | 10.00ms | 10.00ms | 0ns | 10.00ms | 10.00ms | 10.00ms | 10.00ms | 10.00ms | 0.000 | 0.000 | 20 | measured | - | C1 |
| validate / himorime | base | CPU time | 12.00ms | 12.00ms | 0ns | 12.00ms | 12.00ms | 12.00ms | 12.00ms | 12.00ms | 0.000 | 0.000 | 20 | measured | - | C2 |
| validate / himorime | base | Peak RSS | 32.00MiB | 32.00MiB | 0B | 32.00MiB | 32.00MiB | 32.00MiB | 32.00MiB | 32.00MiB | 0.000 | 0.000 | 20 | measured | - | C3 |
| validate / himorime | head | Latency | 10.10ms | 10.10ms | 0ns | 10.10ms | 10.10ms | 10.10ms | 10.10ms | 10.10ms | 0.000 | 0.000 | 20 | measured | - | C1 |
| validate / himorime | head | CPU time | 12.10ms | 12.10ms | 0ns | 12.10ms | 12.10ms | 12.10ms | 12.10ms | 12.10ms | 0.000 | 0.000 | 20 | measured | - | C2 |
| validate / himorime | head | Peak RSS | 33.00MiB | 33.00MiB | 0B | 33.00MiB | 33.00MiB | 33.00MiB | 33.00MiB | 33.00MiB | 0.000 | 0.000 | 20 | measured | - | C3 |

| Rule | Statistic | Minimum difference | Required confidence | Minimum samples | Max CV |
|---|---|---|---|---|---|
| R1 | median | 1.00ms | 95.0% | 10 | 0.5 |
| R2 | median | 2.00MiB | 95.0% | 10 | 0.5 |

| Collection | Metric | Source | Scope | Process aggregation |
|---|---|---|---|---|
| C1 | Latency | example | - | - |
| C2 | CPU time | example | - | - |
| C3 | Peak RSS | example | - | - |

| Field | Value |
|---|---|
| himorime | v0.2.2 |
| Mode | compare |
| OS/architecture | linux/amd64 |
| CPU | Example CPU (4 logical CPUs) |
| Go | - |
| CI | - |
| Seed | 42 |
| Exit code | 0 |
| Command results | 1 passed; 1 improved; 0 inconclusive; 0 over budget; 0 regressed; 0 metric errors |
| Execution errors (commands, benchmarks and suites) | 0 |
| Fail on inconclusive | false |
| Skipped comparisons and budgets | 0 |
| Non-gating comparisons | 0 passed; 0 improved; 0 inconclusive; 0 regressed |
| New suites | 0 |
| Values | Base/head use the rule's statistic; measurement columns use their named statistic. |
| Change and interval | Recorded head-relative-to-base percentages; not a good/bad score. |
| Measurement floor | ≤ is an upper bound; floor-limited differences and intervals are not resolved changes. |
| Base | aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa |
| Head | bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb |
| Base ref | - |
| Base source | - |
| Working tree dirty | false |
| Suite | CLI performance |
