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

Write scenarios must always use the documented `--dry-run` then `--confirm <confirm_token>` sequence. High and critical writes must include `--dangerous` in both steps.
