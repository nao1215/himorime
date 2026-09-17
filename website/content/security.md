---
title: Security
description: yahiko's security boundary, threat model, and how to report a vulnerability.
toc: true
---

## Trust model

A suite executes the commands it declares, with your privileges. Running a
suite is running a script: only run suites you would run as a script.
yahiko's protections bound what a reviewed suite does by accident; they are
not a sandbox for a hostile one.

## In CI

- Run `yahiko ci` on `pull_request` with `permissions: contents: read` and no
  secrets. A pull request's suite and build commands are that pull request's
  code.
- yahiko refuses `pull_request_target`, which runs with the base repository's
  secrets and a write token.
- yahiko never calls the GitHub API, needs no token, and reads the event
  payload only to find the base commit, which must look like a commit SHA.

## Protections

- Commands are argument lists executed without a shell unless `shell: true`
  is set. With a shell, every substituted variable is quoted for that shell.
- `cwd`, `stdin`, `stdout`, `stderr` and report paths are validated: no
  absolute paths, no `..` after a variable, and after symbolic links are
  resolved they must stay inside the repository, the base worktree, or the
  benchmark's `${workdir}`.
- Temporary directories are created with `os.MkdirTemp` (mode 0700). Cleanup
  deletes only directories yahiko created inside its own temporary directory,
  and removes a symbolic link instead of following it.
- The base revision is checked out as a detached worktree with repository hooks
  disabled, and a revision starting with `-` is rejected before it reaches Git.
- On timeout, interrupt, and when a command or hook exits, its whole process
  tree is stopped: a process group on Unix, a Job Object on Windows.
- A shell command that puts a variable inside quotes of its own is rejected,
  and a symbolic link to a missing target is refused rather than followed.
- Reports never contain environment variables or host names, and show command
  lines as written, before `${env:NAME}` is substituted. Error messages and the
  standard error of failing commands are masked for the values of environment
  variables whose names look secret (containing TOKEN, SECRET, PASSWORD, KEY,
  CREDENTIAL, AUTH and similar), including variables a suite's `env` passes to
  a command.

## Reporting a vulnerability

Please report vulnerabilities privately, through the repository's
[Security advisories](https://github.com/nao1215/yahiko/security/advisories/new)
or by email to n.chika156@gmail.com. See
[SECURITY.md](https://github.com/nao1215/yahiko/blob/main/SECURITY.md).
