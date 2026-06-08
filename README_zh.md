# archery-cli

[English](README.md) | [中文](README_zh.md)

AI 代理友好的 [Archery](https://github.com/hhyo/Archery) SQL 审核平台命令行工具。从终端或通过 AI 代理管理 SQL 工单、查询、实例、诊断、binlog、数据归档和数据字典。

## 安装

### CLI 二进制

```bash
# 当前状态：未发布，从源码检出安装
go install ./cmd/archery-cli
```

### Agent Skill

```bash
npx skills add ./skills/archery-cli -y -g
```

## 快速开始

```bash
# 1. 配置
archery-cli auth login --url https://archery.example.com --username <USER> --password <PASS> --region default --dry-run
archery-cli auth login --url https://archery.example.com --username <USER> --password <PASS> --region default --confirm <confirm_token>

# 2. 验证
archery-cli context --compact
archery-cli doctor --compact

# 3. 第一个命令
archery-cli instance list --compact
```

## 用法 / 命令

运行 `archery-cli reference` 获取完整的机器可读命令树。

| 领域 | 命令 |
|------|------|
| SQL 工单 | `workflow list / submit / detail / audit / execute / cancel / sqlcheck` |
| 查询 | `query run / explain / log / favorite / generate` |
| 实例 | `instance list / detail / resource / describe / create / update / delete` |
| 慢查询 | `slowquery review / history / optimize` |
| 诊断 | `diagnostic process / kill / tablespace / locks / transactions` |
| Binlog | `binlog list / parse / purge` |
| 归档 | `archive list / apply / audit / switch / once / log` |
| 数据字典 | `dict tables / table-info / views / triggers / procedures / export` |
| 用户 | `user list / groups / resource-groups` |
| 认证 | `auth login / logout / status` |
| 自更新 | `update` |

## 配置

archery-cli 将配置存储在 `~/.archery-cli/config.json`（文件权限 `0600`）。
凭据只存储在操作系统钥匙串。密码和 token 不会写入配置文件；如果钥匙串不可用，请用环境变量执行一次性命令。

环境变量（覆盖配置文件）：

| 变量 | 用途 |
|------|------|
| `ARCHERY_CLI_URL` | Archery 实例 URL |
| `ARCHERY_CLI_USERNAME` | 用户名 |
| `ARCHERY_CLI_PASSWORD` | 密码 |
| `ARCHERY_CLI_REGION` | 活跃区域名称 |
| `ARCHERY_CLI_INSECURE` | 显式跳过 HTTPS TLS 证书校验 |
| `ARCHERY_CLI_TIMEOUT` | HTTP 超时时间 |
| `ARCHERY_CLI_MAX_RETRIES` | 临时 HTTP 失败的重试次数 |
| `ARCHERY_CLI_USER_AGENT` | 自定义 HTTP User-Agent |
| `NO_COLOR` | 禁用彩色输出 |

## 面向 AI 代理

- **Skill**: [skills/archery-cli/SKILL.md](skills/archery-cli/SKILL.md)
- **CLI 契约**: [.agent/CLI-SPEC.md](.agent/CLI-SPEC.md)
- **安全**: [.agent/SEC-SPEC.md](.agent/SEC-SPEC.md)
- **能力发现**: 运行 `archery-cli reference`
- **预检**: 运行 `archery-cli context` 然后 `archery-cli doctor`
- **危险写操作**: 如果 `reference` 返回 `requiresDangerous`，在 `--dry-run` 和 `--confirm` 两步都必须带 `--dangerous`

## 开发

```bash
# 构建
make build

# 测试
make test

# 代码检查
make lint

# 格式化
make fmt
```

## 许可证

[MIT](LICENSE)

## 贡献

参见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 安全

参见 [SECURITY.md](SECURITY.md)。
