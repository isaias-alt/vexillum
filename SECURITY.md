# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately, not in a public issue.

Use GitHub private vulnerability reporting: open the
[Security tab](https://github.com/isaias-alt/vexillum/security) of the
repository and click **Report a vulnerability**. Include what you found, the
version (`vx --version`), and steps to reproduce it.

You will get a reply as soon as the maintainer can read it. This is a
single-maintainer project, so there is no guaranteed response time.

## Scope

In scope: the `vx` binary and the files it writes under `~/.vexillum/`, the
install script `scripts/install.sh`, and the release artifacts and their
checksums.

Out of scope: vulnerabilities in Claude Code, herdr, tmux, git or `gh`
themselves (report those upstream), and anything that requires an attacker who
already has control of your OS user.

## Soldiers run unsandboxed

Soldiers run Claude Code with `--dangerously-skip-permissions`, as your OS
user, with no container or sandbox. A soldier has the same access to your
machine as you do, including your credentials and files outside the repository.
Landing a mission is the approval gate for what reaches your project's history,
not a security boundary. Do not dispatch against a repository or on a machine
where that would be a problem. See the warning in the
[README](README.md#install).

Reports that boil down to "a soldier can read or write outside its camp" are
known behavior, not vulnerabilities.

## Supported versions

Only the latest release receives security fixes. Please check that the problem
reproduces on it before reporting.
