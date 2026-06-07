# archery-cli

[English](README.md) | [中文](README_zh.md)

AI-Agent-friendly CLI for the [Archery](https://github.com/hhyo/Archery) SQL audit platform. Manage SQL workflows, queries, instances, diagnostics, binlog, data archiving, and data dictionaries from your terminal or via an AI agent.

## Install

### CLI binary

```bash
# npm (recommended)
npm install -g @fatecannotbealtered/archery-cli

# Or download from GitHub Releases
# https://github.com/fatecannotbealtered/archery-cli/releases
```

### Agent Skill

```bash
npx skills add archery-cli -y -g
```

## Quick Start

```bash
# 1. Configure
archery-cli auth login --username <USER> --password <PASS> --region default

# 2. Verify
archery-cli doctor

# 3. First command
archery-cli instance list --compact
```

## Usage / Commands

Run `archery-cli reference` for the full machine-readable command tree.

| Domain | Commands |
|--------|----------|
| SQL Workflows | `workflow list / submit / detail / audit / execute / cancel / sqlcheck` |
| Queries | `query run / explain / log / favorite / generate` |
| Instances | `instance list / detail / resource / describe / create / update / delete` |
| Slow Queries | `slowquery review / history / optimize` |
| Diagnostics | `diagnostic process / kill / tablespace / locks / transactions` |
| Binlog | `binlog list / parse / purge` |
| Archive | `archive list / apply / audit / switch / once / log` |
| Data Dictionary | `dict tables / table-info / views / triggers / procedures / export` |
| Users | `user list / groups / resource-groups` |
| Auth | `auth login / logout / status` |
| Self-Update | `update` |

## Configuration

archery-cli stores config in `~/.archery-cli/config.json` (file permissions `0600`).

Environment variables (override config file):

| Variable | Purpose |
|----------|---------|
| `ARCHERY_CLI_URL` | Archery instance URL |
| `ARCHERY_CLI_USERNAME` | Username |
| `ARCHERY_CLI_PASSWORD` | Password |
| `ARCHERY_CLI_REGION` | Active region name |
| `NO_COLOR` | Disable colored output |

## For AI Agents

- **Skill**: [skills/archery-cli/SKILL.md](skills/archery-cli/SKILL.md)
- **CLI contract**: [.agent/CLI-SPEC.md](.agent/CLI-SPEC.md)
- **Security**: [.agent/SEC-SPEC.md](.agent/SEC-SPEC.md)
- **Capabilities**: run `archery-cli reference`
- **Pre-flight**: run `archery-cli context` then `archery-cli doctor`

## Development

```bash
# Build
make build

# Test
make test

# Lint
make lint

# Format
make fmt
```

## License

[MIT](LICENSE)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md).
