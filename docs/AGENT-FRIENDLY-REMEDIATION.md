# archery-cli 整改清单（对齐模板 spec 版）

> 视角：一个真实 AI agent 端到端跑「overseas(jwt+2FA) 上读 chat_message 最近 10 对问答」踩到的全部坑。
> 基线：本仓 `.agent/`（vendored，`SPEC_VERSION=v1.4`）+ 共享 `ai-native-cli-spec`（CLI-SPEC / SEC-SPEC / REPO-SPEC）。
> 每条都标 **spec 依据** 与归桶，证据带 `file:line` 或实测 curl。

## 对齐结论：三个桶

- **桶 A — 工具违反既有 spec**：spec 已写明，实现没做到 → 改 `archery-cli`，引用条款。
- **桶 B — spec 本身空白**：是系统性、对所有衍生工具都成立的最佳实践，spec 没覆盖 → **回流到模板 `ai-native-cli-spec`**，否则每个种子工具都继承同样的坑。
- **桶 C — 我之前判错，撤回**：经对齐发现是 spec 有意设计，不是 bug。

---

## 桶 C：先纠正自己（避免误导整改）

### C-1　confirm token「写前消费」是 §9 明文设计，不是 bug ❌撤回
- 我上一版把「confirm token 在写成功前就消费、auth 失败也烧掉」列为待修 P2。**错了。**
- **spec 依据**：CLI-SPEC §9「**要在写执行之前标记消费**（中途崩溃宁可保守拒绝重放，也不冒重复风险）」「token 一旦被接受执行写操作，就记录其指纹……任何重放都以 `E_CONFLICT` 拒绝」。`cmd/confirm.go:84` 的行为**正是 spec 要的**——这是无资源版本可绑时的通用安全重试机制。
- **唯一可议的细微点**（最多算 spec 级 nuance，不是工具 bug）：消费点在 `markDryRunOrConfirm` 命令入口、早于 auth；可探讨「挪到真正发出变更请求的边界」以免 auth 失败也烧 token。但 §9 的保守语义优先，**默认维持现状**。本条从待修项移除。

---

## 桶 A：工具违反既有 spec（改 archery-cli）

### A-1　非 JSON / 重定向响应误分类，违反 §6 状态码映射铁律 ［P0｜已证实］
- **现象**：`/query/` 未登录回 `302 Location:/login/`，HTTP client 自动跟随→`200` HTML 登录页→`json.Unmarshal` 撞 `<`→报 `E_SERVER: invalid character '<'`（甚至当成可重试）。
- **证据**：curl 实测 `POST /query/`→`302 /login/`；`GET /login/`→`200 text/html 9250B`。吞掉于 `cmd/query.go:211-212`；自动跟随来自 `internal/api/client.go newHTTPClient`。
- **违反**：CLI-SPEC §6「`401 -> E_AUTH`」「**不要把所有 4xx 都塌缩成** `E_NETWORK`」「**优先按上游错误类型/状态码映射，不要靠匹配人类可读 message**」「把这套映射收敛到**一个**函数里」。一个本质是「未认证」的响应被报成服务器错误，是直球违规。
- **改法**：session 请求**关闭自动重定向**或拦截 `Location:/login/`→映射 `E_AUTH`（exit 4）「会话失效/未登录」。所有「响应非 JSON」分支按 content-type/status 进 §6 映射，错误体附 `status_code`+`content_type`+正文前 256 字符，绝不出现裸 `invalid character '<'`。

### A-2　把「失败」吞成「成功」，违反 §1.6 / §12.7 / §3 envelope 契约 ［P0｜已证实｜系统性］
- **现象**：2FA verify 判成功用 `ar.Status==0 && hasSessionCookie()`——①不看 `resp.StatusCode`（400 放行）②DRF 端点返回 `{"detail"/"字段":[...]}` 无 `status` 字段→`ar.Status` 取零值 0→恒真 ③调用前自注入临时 sessionid→`hasSessionCookie()` 恒真。三者叠加 → **失败被判成功**，带未认证会话继续。
- **证据**：`internal/api/client.go` `complete2FA` 成功分支 / `hasSessionCookie()` / 临时 cookie 注入；`ensureSession` 解析 `/authenticate/` 同为 `_ = json.Unmarshal`。`cmd/diagnostic.go:111,261,319,377` 亦 `_ = json.Unmarshal(data,&items)`。
- **违反**：CLI-SPEC §1.6「写操作在缺少有效……时**必须失败而不是继续执行**」、§12.7「命令失败时优先返回结构化错误，**不输出半截成功 payload**」、§3「`ok` 必须如实反映结果」。对 agent 最致命：它会**基于假成功继续动作**（本次即带未登录会话查询、且一度以为 2FA 通过）。
- **改法**：fail-closed——成功必须**正向证明**：`resp.StatusCode<400` **且** 一个已认证探针通过（带新 cookie 打需登录的 REST 端点拿到本人信息）；DRF 响应按 DRF 形状解析；**删除数据/认证路径上所有 `_ = json.Unmarshal`**，解析错误一律冒泡。

### A-3　2FA verify 漏传上游必填字段 `auth_type` ［P0｜已证实］
- **现象**：`/api/v1/user/2fa/verify/` 要 `engineer + otp + auth_type`，CLI 只发前两个 → 服务端 `400 {"auth_type":["该字段是必填项。"]}`，又被 A-2 吞成成功。
- **证据**：curl 实测缺字段→400；补 `auth_type=totp` 后→`{"status":0,"msg":"ok"}` 登录成功、`/query/` 正常返回数据。代码漏发在 `complete2FA` 的 form。
- **违反**：CLI-SPEC §13 FCC——这条 2FA+session+query 真路径**显然无真实环境用例覆盖**（否则必现）。
- **改法**：verify 补 `auth_type`（认证器=`totp`，值应从账号 2FA 配置/`authenticate` 返回读出，勿写死）。**必须有一个真实开 2FA 的账号做 E2E 断言「真取到数据」**（见 A-7）。

### A-4　context.credentials 不报有效性/过期，且 jwt-region session 零持久化，违反 §16.1 ［P1｜已证实］
- **现象**：overseas 是 jwt，但 `query/dict/diagnostic` 等是 session-only 命令，每次都重新 `/authenticate/`+2FA → **每条 session 命令要一个新 OTP**，无法迭代。
- **证据**：`ExportSessionCookies` 全仓仅 `internal/api/auth.go:60`（auth login）调用；惰性 `ensureSession` 建立的 sessionid 从不 `SaveSession`；`internal/config/config.go` keyring 按 `EffectiveMode` 分支，jwt-region 只存/取 token，从不缓存 session。`context` 的 credentials 只报 `configured/mode/hasSession`。
- **违反**：CLI-SPEC §16.1「可自动刷新的工具应**透明刷新，不让 Agent 操心**」「`context.data.credentials` 应报告**有效性与过期信息**（`valid`/`expires_at`/`refreshable`）」。
- **改法**：**任何**成功登录（含惰性 session、含 jwt-region）都把 sessionid+csrftoken 落 keyring 并复用至过期；`context.credentials` 补 `valid/expires_at`。目标：一次 OTP 后一段时间内同 region 的 session 命令免 OTP。

### A-5　env 账密只覆盖 active region，不跟 `--region` 走 ［P1｜已证实］
- **现象**：`--region overseas` + `ARCHERY_CLI_USERNAME/PASSWORD` → 账密落到 `default`，overseas 仍报缺账密。
- **证据**：`internal/config/config.go:126-153` env override 作用于 `ActiveRegion(cfg)`；`--region` 直到 `cmd/root.go newClient` 才生效。
- **违反**：CLI-SPEC §11 `context` 应如实反映**有效**运行环境/凭证状态——当前 env 覆盖与 `--region` 解析不一致，导致 context 与实际行为错位。
- **改法**：先把**有效 region**（`--region` > `ARCHERY_CLI_REGION` > config default）解析定下来，再对它套所有 env/flag override；冲突时 stderr 显式 warn。

### A-6　只读查询被当 T2 危险写，违反 SEC §1/§3 风险分级 ［P1｜已证实］
- **现象**：`query run`（含纯 `find`/`SELECT`）标 `riskLevel=high`+`write`，读 10 条也要 `--dangerous`+dry-run+confirm 全套。
- **证据**：`cmd/query.go:35 markRiskLevel(queryRunCmd,"high")`、`:34 markWrite`。
- **违反**：SEC-SPEC §1「T0 低危=只读」「§3 危险操作**单列**最高档，默认关」；CLI-SPEC §1.6「查询不改变状态」。把只读与「可 drop」的写混进同一危险门，属风险模型误用。
- **改法**：按解析后的语句语义分级——只读 `find/SELECT` 降 T0/low、免 `--dangerous`、免双步确认；写/DDL 才进危险门。提供只读快路径（如 `query read`）。
  - *注*：若 `/query/` 在某些引擎可执行写 SQL，则保留按「解析出写语句才升档」的细分，而非一刀切高危。

### A-7　声明的发布就绪等级与真实证据不符，违反 §13 ［P1｜强推断］
- **现象**：`doctor`/`reference` 报 `release_readiness=pass`，但 A-1/A-2/A-3 证明 jwt+2FA+session+mongo 这条公开路径从未真跑通。
- **违反**：CLI-SPEC §13「`stable` 至少有一次真实环境 smoke/E2E 记录」「FCC=100% 公开行为」「E2E 必须断言**真的取到数据**而非没报错」（A-2 让「没报错」失去意义）。
- **改法**：补 jwt-region+2FA+session 命令(query/dict)+MongoDB 的端到端用例并断言取到数据；在未补齐前如实降级为 `beta`。

### A-8　标识符 name/id 不统一，违反 §8「ID 用字符串」 ［P2｜已证实］
- **现象**：`instance resource` 对 `--instance` 做 `ParseInt`（要数字 id），`dict/query` 收实例名。
- **证据**：`cmd/instance.go` resource 段 `ParseInt`。
- **违反**：CLI-SPEC §8「所有 ID 使用字符串，即使底层是数字」。
- **改法**：所有 `--instance` 统一接受 `name|id`，内部解析，与 `workflow submit` 的 `--instance/--instance-name` 看齐。

### A-9　auth login json 模式强制三 flag，无视已存配置/env ［P2｜已证实］
- **证据**：`cmd/auth.go:116`；已配 region URL、已设 env 密码仍报必须带 `--url/--username/--password`。
- **违反**：与 SEC-SPEC §4「env 变量是推荐的非交互秘密通道」相悖——既然支持 env，就该能用 env 完成登录。
- **改法**：缺失项回退 config/env，仅三者皆空才报错。

### A-10　必填/枚举 flag 逐个撞，可选值不进 reference ［P2｜已证实］
- **证据**：`cmd/instance.go:520` 先报 `--type is required`，补上又 `:526` 报 `must be one of: database, schema, table, column`。
- **违反**：CLI-SPEC §11「机器能力通过 `reference` 暴露，不要求 agent 解析 help」「params 声明完整」——枚举可选值应在 reference 的 params 里给出。
- **改法**：一次性校验、首个错误即列可选值；params 在 reference 标注 enum。

### A-11　MongoDB 查询语法/示例缺失，违反 §11 examples 要求 ［P2｜已证实］
- **现象**：实测可用 `db.coll.find().sort({_id:-1}).limit(N)`，但 agent 只能盲试；`docs/COMPATIBILITY.md:32` 仅标 `Compatible`。
- **违反**：CLI-SPEC §11「每个命令应带 `examples`（可直接运行）」「`output_schema` 不能是 stub」。
- **改法**：`reference` 按 db-type 给查询语法 + 可运行 example + output_schema；理想再为 mongo 提供结构化 flag（`--find/--sort/--limit/--projection`）替代裸 shell 串，消除 `limit_num` 与 `.limit()` 的重复语义。

---

## 桶 B：spec 本身空白 → 回流到模板 `ai-native-cli-spec`

> 这些不是 archery-cli 独有，而是**所有种子工具**都会踩。改在工具里只救一个；改在模板+`contract.json`+CI 守卫里，才救全体。

### B-1　【新增 CLI-SPEC 铁律】Fail-closed 解析：无法肯定解读为成功的响应一律失败 ［P0 级模板缺口］
- **缺口**：spec 有 §1.6/§12.7「失败别吐半截成功」，但**没有**一条针对「响应解析」的正向规则。A-2 这类「零值 status + 自注入 cookie + 忽略 unmarshal error」能轻松绕过现有条款。
- **建议条款**：「成功必须正向证明，不得由『未观察到失败』推定。① HTTP `status>=400` 一律按 §6 映射为错误；② 响应 content-type 非 JSON（除 `raw`）即错误，禁止喂给 JSON 解析器后凭异常判断；③ 认证/会话成功必须有正向信号（状态码 + 已认证探针 / 凭证实际变化），不得仅凭『cookie 存在』『字段缺省值』。」
- **配套**：`contract.json` 增「禁止 `_ = json.Unmarshal` 于数据/认证路径」的 lint 守卫（`scripts/check-spec.js` 可机扫）。

### B-2　【新增 CLI-SPEC §4 要求】长操作必须有 stderr 进度心跳 ［P1 级模板缺口］
- **缺口**：§4 只说「stderr 可输出进度」，非强制。导致 `instance list --search` 全量翻页（5 行/页、远端 ~1.2s/页、实测 **117s**）期间 agent 完全干等。
  - *注*：该翻页**符合** §8.1「万不得已本地过滤须先取全量」+ §8 `truncated` 标志，所以不是违规；痛点纯在「无反馈的长延迟」，恰是 spec 没规定的。
- **建议条款**：「单次预期耗时 > Ns（建议 5s）或需多页/多请求的操作，**必须**向 stderr 周期性输出结构化进度（如 `{"progress":{"page":N,"elapsed_ms":...}}`），不污染 stdout 契约；并在 reference 标注该命令可能长耗时。」

### B-3　【新增 CLI-SPEC §11 字段】reference 按命令声明传输/认证要求 ［P1 级模板缺口］
- **缺口**：同一 region 下命令隐式分裂成两套 API 面（REST/jwt vs session/web-AJAX），agent 无法预测某命令要不要额外 session 登录（+2FA）。spec 的 reference 没有表达「该命令走哪种传输、需要哪类凭证」的字段。
- **实测分裂**：REST/jwt 即可——`workflow* / user* / instance list,detail,resource,CRUD / auth`；session-only(+可能 2FA)——`query* / dict* / instance describe,test-instance,user-* / diagnostic* / slowquery* / archive*`。
- **建议条款**：reference 每命令增 `transport: rest|session|both` 与 `auth: jwt|session|either`；并要求**认证层自动**按命令所需补齐会话（jwt-region 调 session 命令时透明完成 session 登录并缓存），使 agent 永远只面对「已登录/未登录」二态。这同时把 A-4 升格为模板级承诺。

---

## 优先级总表

| P | 编号 | 桶 | 一句话 | spec 依据 |
|---|------|----|--------|-----------|
| P0 | A-1 | A | 非JSON/重定向误分类成 E_SERVER | CLI §6 |
| P0 | A-2 | A | 失败吞成成功（假阳性/宽松解析） | CLI §1.6 §12.7 §3 |
| P0 | A-3 | A | 2FA verify 漏传 auth_type | CLI §13 |
| P0 | B-1 | B | 缺 fail-closed 解析铁律 | 模板新增 |
| P1 | A-4 | A | session 零持久化 + 凭证状态不全 | CLI §16.1 |
| P1 | A-5 | A | env 账密不跟 --region | CLI §11 |
| P1 | A-6 | A | 只读查询被当 T2 危险写 | SEC §1 §3 / CLI §1.6 |
| P1 | A-7 | A | readiness 声明与证据不符 | CLI §13 |
| P1 | B-2 | B | 长操作无进度心跳 | 模板新增 §4 |
| P1 | B-3 | B | reference 未声明传输/认证 | 模板新增 §11 |
| P2 | A-8 | A | name/id 不统一 | CLI §8 |
| P2 | A-9 | A | auth login 强制三 flag | SEC §4 |
| P2 | A-10 | A | 枚举值不进 reference | CLI §11 |
| P2 | A-11 | A | Mongo 语法/示例缺失 | CLI §11 |
| — | C-1 | C | confirm 写前消费=§9 设计，撤回 | CLI §9 |

---

## 一句话主线

最严重的不是任何单点功能，而是 **A-2 / B-1：「没看到失败就当成功」**——它违反 envelope 最根本的 `ok` 契约，让认证其实失败一路伪装成功，agent 基于假成功继续动作。先在工具里 fail-closed（A-1/A-2），再把这条上升为模板铁律（B-1），收益覆盖所有衍生工具。

## 验收标准
- overseas(jwt+2FA) 上 `archery-cli query run --otp <码>` **一条命令**直接返回数据；一次 OTP 后同 region session 命令免 OTP（A-3/A-4）。
- 任意非 JSON/重定向/字段缺失响应 → 明确 `E_AUTH`/`E_VALIDATION` 带 `status_code`+`content_type`+正文片段，绝无 `invalid character '<'`（A-1/B-1）。
- 只读 `find/SELECT` 无需 `--dangerous`、无需双步（A-6）。
- `--region X` + env 账密 → 确实作用于 X（A-5）。
- 数据/认证路径无 `_ = json.Unmarshal`；E2E 断言「取到数据」，readiness 诚实（A-2/A-7）。
- 模板侧：B-1/B-2/B-3 落入 `ai-native-cli-spec` + `contract.json` + CI 守卫，再 `sync-spec` 回各工具。
