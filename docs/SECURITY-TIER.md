# Security Tier Classification

## Risk Tier: T2 (High)

archery-cli holds writable credentials and can trigger database-impacting operations, including query execution, approved workflow execution, instance deletion, archive purges, binlog purges, and diagnostic thread kills. The worst-case impact is high, so T2 controls apply.

Capabilities by tier:

- T0 baseline: output redaction, `_untrusted` tagging, structured errors
- T1 additions: credential encryption at rest, least privilege, supply chain verification
- T2 additions: high/critical writes require the explicit `--dangerous` gate in addition to `--dry-run` and `--confirm`

## Credential Storage

archery-cli stores credentials in the operating system keyring: macOS Keychain, Windows Credential Manager, or Linux Secret Service. This is the credential encryption path for T2 operation.

If the platform keyring is unavailable, `auth login` refuses to persist credentials. Users may still provide `ARCHERY_CLI_URL`, `ARCHERY_CLI_USERNAME`, and `ARCHERY_CLI_PASSWORD` for one-shot commands; those values stay in process memory and are not written to `~/.archery-cli/config.json`.

**Mitigations in place:**
- Config file permissions are restricted to `0600` (owner read/write only) on POSIX systems
- The `.gitignore` excludes `config.json` and local env files to prevent accidental commits
- `doctor` reports whether the OS keyring is available
- SECURITY.md warns users to protect credentials and use env vars for one-shot operation when keyring is unavailable

## Dangerous Operation Gate

High and critical write commands are off by default. They require `--dangerous` in both steps:

```bash
archery-cli query run --instance prod --db app --sql "UPDATE ..." --dangerous --dry-run
archery-cli query run --instance prod --db app --sql "UPDATE ..." --dangerous --confirm <confirm_token>
```

This second gate is separate from the confirm token. It prevents an agent from escalating from a normal write flow into a dangerous operation without an explicit user-approved command shape.
