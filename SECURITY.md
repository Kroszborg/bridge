# Security policy

Bridge handles API keys, phone numbers, message text and the phones that send them, so security
reports get our highest priority.

## Reporting a vulnerability

**Do not open a public issue, discussion or pull request.** Report privately, either:

* by email to **abhimanpanwar6@gmail.com**, with "Bridge security" in the subject, or
* through GitHub's
  [private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
  (the **Report a vulnerability** button on the repository's **Security** tab).

Please include:

* what you found and the impact you expect (for example, which data or account an attacker could
  reach);
* steps to reproduce, or a proof of concept;
* the affected component (API, worker, dashboard, Android app, SDK, CLI, Docker images) and the
  version or commit, or whether you found it on hosted Bridge;
* any logs, requests or screenshots that help, with real phone numbers and message text removed.

### What to expect

| Step | Target |
| --- | --- |
| Acknowledge your report | within 3 working days |
| First assessment and severity | within 7 days |
| Fix for critical and high severity issues | within 30 days, sooner where we can |
| Fix for other issues | in a following release |

We keep you updated while we work on it, agree a disclosure date with you, and credit you in the
release notes and advisory unless you prefer otherwise. Hosted Bridge is patched as soon as a fix is
ready; self-hosted users are told through a GitHub security advisory and the changelog.

### Testing guidelines

Test against your own self-hosted server or your own hosted workspace. Do not access other people's
data, send SMS to numbers you do not own, degrade the hosted service (no load or denial-of-service
testing), or use social engineering. Stop and report as soon as you have shown the issue. We will not
pursue good-faith research that follows these rules.

## Supported versions

| Version | Security fixes |
| --- | --- |
| 1.0.x (latest release) | Yes |
| `main` | Yes |
| Releases before 1.0 | No; upgrade to 1.0 |

Hosted Bridge always runs the latest release.

## Scope

In scope: the API server and worker, the dashboard, the Android gateway app, the Docker images,
the TypeScript SDK, `bridgectl` and its MCP server in this repository, and hosted Bridge
(`bridge.kroszborg.co`, `dashboard.bridge.kroszborg.co`, `api.bridge.kroszborg.co`).

Out of scope: weaknesses in your own deployment's infrastructure, carrier or SMS provider behaviour,
reports that need an already compromised server, database or unlocked phone, missing best-practice
headers without a demonstrated impact, and automated scanner output without a working exploit.

## How Bridge protects data

The threat model, every credential, rate limit, retention period and the hosted infrastructure are
described in the [security model](docs/security/README.md).
