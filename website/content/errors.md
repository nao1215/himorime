---
title: Error codes
description: Stable error codes for failures to run a himorime measurement.
---

Error codes identify a failure in himorime itself or in the work needed to measure a suite. They use `HMR` and four digits; the first digit is the process exit status.

Exit status `1` has no error code. It means that measurement succeeded and a budget or regression check failed, so it is a result for CI to act on rather than a himorime error.

<!-- BEGIN GENERATED: error-codes -->
| Code | Name | Meaning |
|---|---|---|
| `HMR2001` | invalid suite | The suite file is missing, malformed, or violates the schema. |
| `HMR3001` | invalid command line | The command name, flag, or command-line argument is invalid. |
| `HMR4001` | execution failed | A command, hook, build, Git operation, or cleanup step could not run. |
| `HMR5001` | internal error | himorime encountered an unexpected internal error. |
| `HMR6001` | metric unavailable | A requested metric cannot be measured on this platform. |
<!-- END GENERATED: error-codes -->

The assigned codes are intentionally small and describe current failure categories. New codes are added when a distinct, actionable failure needs one; future cases are not reserved in advance.

When a command fails, the code is printed at the start of the diagnostic line. Use the exit status for scripting and the code when routing or explaining a failure.
