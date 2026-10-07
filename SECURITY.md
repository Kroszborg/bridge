# Security policy

Bridge handles credentials and phone numbers, so we treat security reports as the highest priority.

## Reporting a vulnerability

**Do not open a public issue.** Report privately through GitHub's
[private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
(the **Report a vulnerability** button on the repository's **Security** tab).

Include what you found, how to reproduce it, the affected version or commit, and the impact you
expect. We aim to acknowledge reports within 3 working days and to agree on a disclosure timeline
with you. We credit reporters unless you prefer otherwise.

## Supported versions

Bridge is pre-1.0. Only the latest release (and `main`) receives security fixes.

## Scope

In scope: the API server, worker, dashboard, Android gateway, Docker images and SDKs in this
repository. Out of scope: vulnerabilities in your own deployment's infrastructure, carrier or
provider behaviour, and reports that require a compromised device or server.

## How Bridge protects data

The security model is documented in [docs/security/README.md](docs/security/README.md).
