"""Mutation testing for the verdict switch of internal/stats/compare.go.

gremlins counts the expressions of switch cases as not covered (Go coverage
blocks start inside the case body), so it never mutates the conditions that
decide regression, improved, pass and inconclusive. This script applies those
mutants one at a time, runs the stats tests against each, and fails when a
mutant survives that is not listed as equivalent, either exactly or in
practice (it differs only at a boundary no bootstrap reaches). The source
file is restored afterwards, also on failure.

Run it from the repository root: python3 scripts/verdict_mutants.py
"""
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "internal" / "stats" / "compare.go"

REG = "case degradation > o.MaxPercent+epsilon && c.ProbRegression >= o.Confidence:"
IMP = "case degradation < -o.MaxPercent-epsilon && c.ProbImprovement >= o.Confidence:"
PASS = "case 1-c.ProbRegression >= o.Confidence:"
MINDIFF = "case o.MinDifference > 0 && math.Abs(c.Difference) < o.MinDifference:"

# (original, mutant, why it is equivalent or None when a test must kill it)
MUTANTS = [
    ("c.Base.Count < o.MinSamples ||", "c.Base.Count <= o.MinSamples ||", None),
    ("c.Base.Count < o.MinSamples ||", "c.Base.Count >= o.MinSamples ||", None),
    ("|| c.Head.Count < o.MinSamples:", "|| c.Head.Count <= o.MinSamples:", None),
    ("|| c.Head.Count < o.MinSamples:", "|| c.Head.Count >= o.MinSamples:", None),
    ("case noisy:", "case !noisy:", None),
    ("case len(changes) == 0:", "case len(changes) != 0:", None),
    (MINDIFF, MINDIFF.replace("MinDifference > 0", "MinDifference >= 0"), "with min_difference 0 no absolute difference is below it"),
    (MINDIFF, MINDIFF.replace("MinDifference > 0", "MinDifference <= 0"), None),
    (MINDIFF, MINDIFF.replace("< o.MinDifference:", "<= o.MinDifference:"), None),
    (MINDIFF, MINDIFF.replace("< o.MinDifference:", ">= o.MinDifference:"), None),
    (REG, REG.replace("degradation >", "degradation >="), "differs only at a change of exactly the tolerance plus epsilon"),
    (REG, REG.replace("degradation >", "degradation <="), None),
    (REG, REG.replace("MaxPercent+epsilon", "MaxPercent-epsilon"), "in practice: the counted resamples still exclude a change at the tolerance, so the probability gate decides alike"),
    (REG, REG.replace("ProbRegression >=", "ProbRegression >"), None),
    (REG, REG.replace("ProbRegression >=", "ProbRegression <"), None),
    (IMP, IMP.replace("degradation <", "degradation <="), "differs only at a change of exactly the tolerance plus epsilon"),
    (IMP, IMP.replace("degradation <", "degradation >="), None),
    (IMP, IMP.replace("-o.MaxPercent-epsilon", "o.MaxPercent-epsilon"), "in practice: an observed change inside the tolerance does not come with most resamples beyond it on the better side"),
    (IMP, IMP.replace("-o.MaxPercent-epsilon", "-o.MaxPercent+epsilon"), "differs only within epsilon of the tolerance"),
    (IMP, IMP.replace("ProbImprovement >=", "ProbImprovement >"), None),
    (IMP, IMP.replace("ProbImprovement >=", "ProbImprovement <"), None),
    (PASS, PASS.replace(">=", ">"), None),
    (PASS, PASS.replace(">=", "<"), None),
    (PASS, PASS.replace("1-", "1+"), None),
]


def main() -> int:
    original = SOURCE.read_text()
    killed, survivors = 0, []
    try:
        for old, new, equivalent in MUTANTS:
            if original.count(old) != 1:
                print(f"compare.go no longer contains exactly one {old!r}; update this script", file=sys.stderr)
                return 2
            SOURCE.write_text(original.replace(old, new))
            result = subprocess.run(["go", "test", "-count=1", "./internal/stats/", "-rapid.nofailfile"], cwd=ROOT, capture_output=True, text=True, check=False)
            if result.returncode != 0:
                killed += 1
                print(f"KILLED     {new}")
            elif equivalent:
                print(f"EQUIVALENT {new} ({equivalent})")
            else:
                survivors.append(new)
                print(f"LIVED      {new}")
    finally:
        SOURCE.write_text(original)
    print(f"{killed} killed, {len(survivors)} lived, {len(MUTANTS) - killed - len(survivors)} equivalent")
    return 1 if survivors else 0


if __name__ == "__main__":
    sys.exit(main())
