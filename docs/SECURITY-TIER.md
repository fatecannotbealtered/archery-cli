# Security Tier Classification

## Risk Tier: T1 (Medium)

archery-cli holds writable credentials (JWT tokens) and can modify external state (submit/approve/execute SQL workflows, manage instances). It does NOT execute arbitrary SQL directly (that is done server-side by Archery/goInception).

Capabilities by tier:

- T0 baseline: output redaction, `_untrusted` tagging, structured errors
- T1 additions: credential encryption at rest, least privilege, supply chain verification
- T2 N/A: no irreversible account-level operations from CLI itself

## Known Gaps

### Plaintext Credential Storage

archery-cli currently stores credentials (JWT tokens) in `~/.archery-cli/config.json` as plaintext. This does not meet the T1 requirement for credential encryption at rest.

**Mitigations in place:**
- Config file permissions are restricted to `0600` (owner read/write only) on POSIX systems
- The `.gitignore` excludes `config.json` to prevent accidental commits
- SECURITY.md warns users to protect config files

**Planned resolution:**
- OS keyring integration (macOS Keychain, Windows Credential Manager, Linux Secret Service) is planned for a future release
- This will move credential storage from plaintext files to the operating system's secure credential store
- Until then, users should ensure their home directory has appropriate permissions and avoid storing credentials on shared systems
