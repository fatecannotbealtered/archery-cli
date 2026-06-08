# Open Source Release Checklist

Security and compliance gate before the first public push.

## Security

- [ ] No hardcoded secrets, API keys, passwords, or tokens in source code
- [ ] No credentials in git history (`git log --all -p | grep -i 'password\|secret\|token\|key'`)
- [ ] Config file template (`config.example.json`) has placeholder values only
- [ ] `.gitignore` excludes `config.json`, `.env`, and other credential files
- [ ] SECURITY.md is present with vulnerability reporting instructions
- [ ] `docs/SECURITY-TIER.md` documents risk classification and keyring-only credential storage

## Dependencies

- [ ] All dependencies audited (`go mod tidy && go vet ./...`; `npm audit --audit-level=high`)
- [ ] No known CVEs in dependencies (check with `govulncheck ./...`)
- [ ] LICENSE file is present and correct
- [ ] All transitive dependencies are compatible with the project license

## Code Quality

- [ ] All tests pass (`go test ./...`)
- [ ] No `TODO` or `FIXME` comments referencing security issues
- [ ] No debug logging that could leak credentials
- [ ] Error messages do not expose internal infrastructure details

## Documentation

- [ ] README.md has install, usage, and contributing sections
- [ ] CONTRIBUTING.md explains how to contribute
- [ ] CHANGELOG.md follows Keep a Changelog format
- [ ] `docs/COMPATIBILITY.md` documents verified platform versions

## CI/CD

- [ ] CI pipeline runs tests on pull requests
- [ ] CI runs lockfile install and high-severity npm audit
- [ ] Release pipeline builds binaries for all supported platforms
- [ ] No secrets in CI configuration files
