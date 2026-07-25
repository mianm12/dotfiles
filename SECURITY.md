# Security Policy

## Supported versions

Only the current `main` branch is supported. This repository has not published a stable release and does not
maintain older branches or tags with security fixes.

## Reporting a vulnerability

Please use GitHub
[Private Vulnerability Reporting](https://github.com/mianm12/dotfiles/security/advisories/new). Do not open a
public Issue for a suspected vulnerability.

Include:

- the affected command, commit and platform;
- the security or data-integrity impact;
- a minimal, synthetic reproduction;
- whether filesystem mutation occurred;
- any suggested mitigation.

Do not upload real HOME contents, private modules, machine configuration, state, lock files, credentials or
other secrets. Replace them with isolated temporary paths and synthetic fixtures.

Reports are handled on a best-effort basis. This personal project does not promise a fixed response or
remediation SLA. Confirmed fixes should become a sanitized regression test and, when mechanically enforceable,
a permanent repository gate.
