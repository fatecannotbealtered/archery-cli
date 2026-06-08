# Security Policy

## Risk Tier

**T2 (High)** -- archery-cli holds writable credentials and can trigger database-impacting operations, including query execution, approved workflow execution, instance deletion, and diagnostic thread kills. See [docs/SECURITY-TIER.md](docs/SECURITY-TIER.md) for details.

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes |

## Reporting a Vulnerability

**Do not report security vulnerabilities through public GitHub issues.**

Instead, report vulnerabilities by emailing [security@fatecannotbealtered.github.io](mailto:security@fatecannotbealtered.github.io) or using [GitHub private vulnerability reporting](https://github.com/fatecannotbealtered/archery-cli/security/advisories/new). Please include:

- A description of the vulnerability
- Steps to reproduce the issue
- Potential impact
- Any suggested fix (optional)

You should receive an acknowledgment within **48 hours**. We will work with you to understand the issue and coordinate a fix before any public disclosure.

## Response Timeline

| Step | Target |
|------|--------|
| Acknowledgment of report | 48 hours |
| Initial assessment | 5 business days |
| Fix or mitigation | Depends on severity |

## Security Best Practices for Users

When using archery-cli:

- **Use HTTPS**: Always connect to Archery instances over HTTPS. Never send credentials over unencrypted connections.
- **Rotate tokens**: Regularly rotate your API tokens and credentials. If you suspect a token has been compromised, rotate it immediately.
- **Least privilege**: Grant only the minimum permissions required. Do not use admin-level credentials for routine operations.
- **Keep updated**: Run `archery-cli update` regularly to get the latest security patches.
- **Protect credentials**: archery-cli stores credentials only in the OS keyring. Passwords and tokens are not written to the config file; if the keyring is unavailable, use environment variables for one-shot commands and never commit config files.
