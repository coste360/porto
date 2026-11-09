# Security policy

Porto handles personal data of boat owners and guests, contracts, payments and access to marina facilities. We take
security reports seriously and thank everyone who reports a problem responsibly.

## Reporting a vulnerability

**Do not open a public issue, discussion or pull request for a security problem.**

Write to **security@coste360.it** with:

- the affected version (`porto version`, the *App desktop* settings page or the image tag) and edition (server,
  Docker image, desktop app);
- a description of the problem and its impact;
- the steps to reproduce it, or a proof of concept;
- whether the problem is already known to others.

You can also use GitHub's private vulnerability reporting (*Security → Report a vulnerability*) on
`github.com/coste360/porto`, if it is enabled for the repository.

Please do not include real personal data in the report, do not access or modify data that is not yours, and test
only against your own installation. A public demo instance is shared with other people even though it is reset
every night: do not run automated scanners or denial-of-service tests against it.

What to expect:

- an acknowledgement of the report, usually within five working days;
- an assessment and, when the problem is confirmed, a fix in a new release, coordinated with you;
- credit in the release notes if you wish, once the fix is published.

## Supported versions

Porto follows [Semantic Versioning](https://semver.org/). Security fixes are published as new patch releases.

| Version | Security fixes |
|---|---|
| Latest minor release (`x.y`, newest `y`) | Yes |
| Previous minor releases | Only for critical problems, at Coste360's discretion |
| Development builds (`main`, `dev`, pre-releases) | No |

Upgrade to the latest patch release of your minor version to receive fixes: the Docker image tag `x.y` always points
to it, and the desktop app shows a banner when a newer version is published (if the version check is on).

## Scope

In scope: the Porto server and its APIs, the admin panel, the public booking page, widget and customer area, the
public marina profile, the desktop app and its local-network pairing, the container image and the deployment
templates in `deploy/`.

Out of scope: vulnerabilities in third-party services configured by a marina (payment gateways, SMTP providers,
e-signature services), problems that need physical access to an unlocked office computer, and reports produced only
by automated tools without a demonstrated impact.

## Handling of secrets

Porto never logs emails, phone numbers, documents, tokens or secrets, and contains no telemetry. If you find any of
these in a log, an event payload, a webhook or an error message, report it as a security problem.
