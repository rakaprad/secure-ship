# Security Policy

## Supported versions

Only the latest commit on `main` (and the image built from it) receives
security fixes. Images are tagged `sha-<commit>` and are never overwritten.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

Report privately through GitHub:
**Security → Report a vulnerability** on
<https://github.com/rakaprad/secure-ship/security/advisories/new>.

Include what you found, how to reproduce it, and the impact you expect.
You can expect an acknowledgement within 5 working days. Once a fix is
available, the advisory will be published with credit to the reporter
unless you prefer to stay anonymous.

## Scope

In scope:

- The Go service in `cmd/` and `internal/`
- The container image published to `ghcr.io/rakaprad/secure-ship`
- The CI pipeline in `.github/workflows/`
- The Kubernetes manifests and Kyverno policies in `deploy/`

Out of scope: findings that require a compromised cluster or GitHub account,
and denial of service through traffic volume alone (the service is a
reference implementation, not a hosted product).

## Verifying releases

Every image published from `main` is signed with cosign (keyless, GitHub
OIDC) and carries an SPDX SBOM attestation. See
[README.md](README.md#verify-a-signed-image) for the verification commands.
