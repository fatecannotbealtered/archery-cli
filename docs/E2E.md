# End-to-End Test Environment

archery-cli talks to a live Archery server for integration testing. Unit tests must not require a live server.

## Required Environment

Set these variables before running E2E checks:

```bash
export ARCHERY_CLI_URL=https://archery.example.com
export ARCHERY_CLI_USERNAME=<user>
export ARCHERY_CLI_PASSWORD=<password>
export ARCHERY_CLI_REGION=e2e
```

The account should use the least privilege needed for the scenario being tested. Do not use production administrator credentials for routine E2E runs.

`auth login` persists tokens only in the OS keyring. If the test host has no working keyring, skip the login step and run one-shot commands with the environment variables above.

## Smoke Flow

```bash
archery-cli auth login --url "$ARCHERY_CLI_URL" --username "$ARCHERY_CLI_USERNAME" --password "$ARCHERY_CLI_PASSWORD" --region "$ARCHERY_CLI_REGION" --dry-run
archery-cli auth login --url "$ARCHERY_CLI_URL" --username "$ARCHERY_CLI_USERNAME" --password "$ARCHERY_CLI_PASSWORD" --region "$ARCHERY_CLI_REGION" --confirm <confirm_token>
archery-cli context --compact
archery-cli doctor --compact
archery-cli reference --compact
archery-cli instance list --limit 1 --compact
```

## Two-Factor Accounts

When the test account has 2FA enabled, the smoke flow above stops at
`E_2FA_REQUIRED` (exit 9) unless a second factor is supplied. Two ways:

```bash
# Interactive: a fresh code, valid ~30s
archery-cli auth login ... --otp 123456 --confirm <confirm_token>

# Unattended: store the TOTP seed once, then no code is ever needed
archery-cli auth login ... --totp-secret "$ARCHERY_CLI_2FA_SECRET" --confirm <confirm_token>
```

For CI, prefer the env channel and skip `auth login` entirely — export
`ARCHERY_CLI_USERNAME`, `ARCHERY_CLI_PASSWORD` and `ARCHERY_CLI_2FA_SECRET` and
run one-shot commands. That path also works on hosts with no OS keyring, which
is the usual CI case.

Do not point these at an account whose 2FA protects anything that matters: a
stored seed puts both factors on the runner.

Write scenarios must always use the documented `--dry-run` then `--confirm <confirm_token>` sequence. High and critical writes must include `--dangerous` in both steps.
