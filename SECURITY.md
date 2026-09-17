# Security policy

## Threat model

yahiko executes the commands a suite declares, with the privileges of the user
who runs it. **A suite is a script.** Only run suites you would run as a
script, and review suite changes like code changes.

What yahiko protects against:

- **Accidental shell interpretation.** Commands are argument lists executed
  without a shell. `shell: true` is opt-in, and every variable substituted into
  a shell command is quoted for that shell (values `cmd.exe` cannot quote are
  refused).
- **Paths escaping the project.** `cwd`, `stdin`, `stdout`, `stderr` and report
  paths may not be absolute or climb out with `..`; after symbolic links are
  resolved they must stay inside the Git repository, the temporary base
  worktree, or the benchmark's `${workdir}`. A symbolic link to a missing
  target is refused rather than followed.
- **Destructive cleanup.** yahiko deletes only directories it created inside
  its own private temporary directory (created with mode 0700), and removes a
  symbolic link rather than following it.
- **Leftover processes.** On timeout, interrupt, and when a command or hook
  exits, its whole process tree is stopped: a process group on Unix, a Job
  Object on Windows.
- **Double quoting.** A shell command that places a variable inside quotes of
  its own is rejected, because the value's quoting would no longer hold.
- **Leaking secrets into reports and logs.** Reports never contain environment
  variables or host names and show commands as written, before `${env:NAME}`
  is substituted. Error messages and the standard error of failing commands
  mask values of environment variables whose names look secret, whether yahiko
  inherited them or the suite's `env` passed them to a command.
- **Touching the user's repository.** The base revision is checked out as a
  detached worktree with repository hooks disabled and removed afterwards; a
  revision starting with `-` is refused.
- **Privileged CI contexts.** `yahiko ci` refuses `pull_request_target` and
  needs no token or secret.

What yahiko does not protect against:

- A hostile suite. It can run any program, including one that deletes files,
  from a hook or a command.
- A hostile program under test. Measured commands run with your privileges.
- Secrets printed by a command in a form other than the variable's exact value.

Recommended CI setup: trigger on `pull_request`, grant only
`contents: read`, pass no secrets to the benchmark job, and check out with
`persist-credentials: false`.

## Supported versions

Only the latest release receives fixes, including security fixes.

## Reporting a vulnerability

Please report vulnerabilities privately, not in public issues or pull requests:

- GitHub: [Report a vulnerability](https://github.com/nao1215/yahiko/security/advisories/new)
- Email: n.chika156@gmail.com

Include the yahiko version (`yahiko version`), the operating system and
architecture, the suite and command line, and what happened. yahiko is
maintained in spare time, so there is no guaranteed response time; reports are
acknowledged, fixed in a new release, and credited unless you prefer otherwise.

## Verifying releases

Release checksums are signed with cosign keyless signing, every archive has an
SPDX SBOM, and build provenance is attested through GitHub's OIDC token. See
[Verify a release](https://nao1215.github.io/yahiko/install/#verify-a-release).
