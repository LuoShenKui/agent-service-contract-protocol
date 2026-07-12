# Agent Service Contract Protocol（ASCP）

[![Protocol](https://img.shields.io/badge/Protocol-Draft%200.2-orange.svg)](spec/ASCP-0.2.md)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8.svg)](go.mod)

**ASCP 是面向“平台自营 Agent”的开放服务协议：完整、低风险的请求可以一次问答完成；付费、价格不确定、不可逆或高影响任务则进入签名服务合同流程。**

外部 Agent 为了“读取最新一封邮件”，不应先加载 Gmail 全部搜索、读取、线程、标签、附件等内部工具定义。它只需要提出一个紧凑任务，Gmail Agent 自己理解平台字段、验证权限并返回结果。外部 Agent 要求发送邮件、携带附件、花钱或使用更高权限时，平台才要求签名报价、独立批准、账单校验、幂等执行和审计回执。

> **项目状态：** `v0.2.0-draft.1` 是可运行的协议草案和 Go 参考实现，适合公开评审、互操作实验和受控环境集成。它尚未经过独立安全认证、支付机构认证或大规模多厂商互操作认证。

## 两种流程

### 一、简略流程：一次问答

```text
外部 Agent                          平台自营 Agent
    │                                     │
    │ “读取我的最新一封邮件”               │
    │────────────────────────────────────>│
    │ 平台内部完成鉴权、权限和策略检查       │
    │<────────────────────────────────────│
    │ 结果＋签名回执                        │
```

接口：

```http
POST /v1/invoke
```

对于“免费、只读、参数完整”的任务，这是**一次请求、一次响应**。不需要先调用 Options，不需要报价，不需要即时支付，也不需要幂等键。平台内部仍然必须完成：

- 调用方身份与用户委托校验；
- 最小权限检查；
- 输出大小控制；
- 审计记录；
- 结果回执签名。

平台也可以允许带副作用的简略流程，但前提是价格、权限、授权和结算关系都已由长期协议固定。此时必须使用 `Idempotency-Key`，并按平台声明提供长期授权或账单凭证。

### 可选的 Options

```http
POST /v1/options
```

Options 不是必经步骤。只有外部 Agent 不清楚下列信息时才调用：

- 应选择哪个任务意图；
- 还缺哪些参数；
- 当前能否走简略流程；
- 需要哪些权限；
- 能否传文件；
- 支持哪些账单方式；
- 是否必须进入签名合同流程。

Options 完全无副作用：不创建报价、不计费、不产生授权、不创建任务。普通 HTTP `OPTIONS /v1/invoke` 只返回传输层提示。

### 二、正常流程：签名服务合同

```text
外部 Agent                          平台自营 Agent
    │                                     │
    │ Negotiate：这件事能不能做？          │
    │────────────────────────────────────>│
    │ 最小参数、权限、文件和账单选择         │
    │<────────────────────────────────────│
    │                                     │
    │ Prepare：这是具体需求和约束           │
    │────────────────────────────────────>│
    │ 签名报价＋副作用预览                   │
    │<────────────────────────────────────│
    │                                     │
    │ 外部验签、取得用户批准、准备结算关系    │
    │                                     │
    │ Commit：接受这一份确定合同             │
    │────────────────────────────────────>│
    │ 唯一任务＋结果＋回执＋审计              │
    │<────────────────────────────────────│
```

平台比外部模型更理解自己的内部字段、业务规则、数据来源、API 和执行系统。因此外部 Agent 只描述目标，平台 Agent 自己拆解内部操作，并对外返回完成当前任务所需的最小合同。

## 平台能力列表

平台在以下地址发布可缓存的紧凑能力目录：

```http
GET /.well-known/ascp/capabilities
```

每项能力只包含：

- 意图和任务版本；
- 简短说明；
- 支持简略流程还是合同流程；
- 副作用与风险等级；
- 所需权限；
- 支持的账单模式；
- 参数名称，而不是完整 Schema；
- 文件策略；
- 输出方式；
- 是否支持任务级 Options。

完整参数 Schema 只在选定任务后，通过 Options 或 Negotiate 按需返回。这样不会把数十个、数百个无关工具定义塞进模型上下文。

## 账单不等于即时支付

ASCP 支持以下模式：

| 模式 | 含义 |
|---|---|
| `free` | 免费，不产生计费记录 |
| `pay_now` | 单次令牌化授权，先预留、成功后结算 |
| `prepaid_balance` | 从已有预付余额扣除 |
| `subscription` | 记录到已有订阅或额度 |
| `postpaid_account` | 记录到获批的后付费账户 |
| `monthly_invoice` | 加入月度账单 |
| `clearing_account` | 通过平台统一清算账户结算 |
| `sponsored` | 由已批准的赞助方承担 |
| `external_settlement` | 在 ASCP 之外按商业合同结算 |

只有签名 `BillingTerms` 明确要求单次授权时，Commit 才需要 `BillingAuthorization`。订阅、企业月结、预付账户、赞助或统一清算可以只提供不透明的长期 `arrangement_ref`，并不要求每次即时付款。

ASCP 不传输银行卡、银行账户、钱包密钥等原始或可复用凭证。真实部署通过适配器连接支付机构、企业账单、预付账本、内部成本分摊或其他结算协议。

## 文件与附件

文件字节不应该进入模型上下文，也不应该反复复制进 JSON。

```text
1. 客户端先声明文件名、类型、大小、SHA-256 摘要和用途；
2. 平台返回短期、单文件、绑定所有者的上传凭证；
3. 客户端把字节上传到受控地址；
4. 平台校验身份、令牌、媒体类型、长度、摘要、有效期、就绪状态和病毒扫描；
5. Direct 或 Contract JSON 只携带 FileRef；
6. 能影响合同任务的每个 FileRef 都进入报价签名。
```

主要接口：

```text
POST /v1/files/prepare-upload
PUT  /v1/files/{file_id}/content
GET  /v1/files/{file_id}
GET  /v1/files/{file_id}/content
```

参考邮件 Agent 支持最多十个附件，并通过 Artifact 引用返回结果，不会把所有附件再次塞进响应。

## 可靠性边界

在“有副作用或有商业关系的服务调用”范围内，ASCP 明确规定：

- **失败关闭：** 不符合简略流程的任务必须返回 `contract_required`，不能悄悄执行。
- **签名条款：** 价格、价格上限、账单模式、文件、回调、副作用、权限、数据用途、确认方式、时间、Actor 和 Principal 全部进入签名报价。
- **独立授权：** 提示词、邮件正文、网页内容和模型输出不能创造权限；批准证据由独立系统验证并绑定具体请求或报价摘要。
- **条件式与强制幂等：** 只读 Direct 可以省略幂等键；任何可能重复副作用或计费的路径必须使用幂等键；结果不确定时进入对账，不能盲目重试。
- **多账单模式：** 即时付款只是其中一种，不把所有平台服务强行建模成刷卡。
- **摘要绑定文件：** 文件字节单独传输，确定元数据进入合同。
- **签名回执和审计：** Direct 调用与 Contract 任务都产生签名回执，并锚定到追加式签名哈希链。
- **最小数据搬运：** 邮件线程、文档、视频和大结果可以留在原平台，通过权限受控引用使用。

## 与 MCP、A2A 的关系

ASCP 不是所有场景下替代 MCP 或 A2A。

| 协议或层 | 最适合解决的问题 |
|---|---|
| 普通 API / SQL | 平台内部确定性的原子操作 |
| MCP | 向模型或 Agent Runtime 暴露有限工具和资源 |
| A2A | Agent 发现、通信、委派和进度 |
| **ASCP** | 服务接受、签名条款、授权、账单、文件、幂等、执行证据和审计 |

平台自营 Agent 内部仍可以使用 SQL、普通 API、队列、规则、模型或 MCP；外部也可以通过 A2A 发现它。ASCP 负责的是服务交易边界：外部不需要理解平台内部所有工具，却能获得可验证、可计费、可审计的结果。

详见 [协议比较](docs/comparison.md)。

## 仓库结构

```text
cmd/ascp-server/          可运行参考服务
cmd/ascp-client/          简略流程＋完整合同示例客户端
cmd/ascp-conformance/     对已部署服务执行一致性检查
internal/email/           平台自营邮件 Agent 示例
pkg/ascp/                 协议类型、校验、金额、签名
pkg/server/               HTTP 引擎、路由、幂等、文件、状态机
pkg/client/               Go 客户端和验签工具
pkg/billing/              账单适配边界与确定性模拟实现
pkg/audit/                签名追加式哈希链
schemas/                  JSON Schema 2020-12
openapi/                  OpenAPI 3.1
spec/                     核心规范、安全、账单、错误、状态机
examples/                 简略、合同、账单和文件示例
conformance/              机器可读一致性用例
deploy/                    PostgreSQL 与 Kubernetes 生产蓝图
```

## 快速运行

要求：

- Go 1.23 或更高版本；
- Python 3，以及 `PyYAML`、`jsonschema`；
- 运行 curl 示例时可选安装 `jq`。

执行完整检查：

```bash
make check
```

启动参考服务：

```bash
go run ./cmd/ascp-server
```

执行一次免费读取和一次签名发送：

```bash
go run ./cmd/ascp-client -to recipient@example.com
```

携带附件：

```bash
go run ./cmd/ascp-client \
  -to recipient@example.com \
  -attachment ./README.zh-CN.md
```

使用订阅，而不是即时支付：

```bash
go run ./cmd/ascp-client \
  -to recipient@example.com \
  -billing subscription \
  -arrangement-ref subscription_demo
```

检查一个已部署服务：

```bash
go run ./cmd/ascp-conformance \
  -base-url http://localhost:8080 \
  -token ascp-demo-token
```

## 最小 Direct 请求

```json
{
  "intent": "email.latest.read",
  "parameters": {
    "include_body": true
  }
}
```

参考服务的只读能力不要求 Options、报价、支付或幂等键。

## 生产边界

参考程序故意使用：固定演示令牌、内存存储、模拟批准、模拟账单、临时签名密钥和进程内同步执行。公网生产必须替换为：

- OAuth/OIDC、DPoP 或 mTLS 委托；
- 持久化、租户隔离的数据库与幂等存储；
- KMS/HSM 签名与密钥轮换；
- 真实账单、支付和对账系统；
- Outbox、队列和异步 Worker；
- 不可变审计存储；
- 隔离对象存储、病毒扫描和内容解析沙箱；
- 回调 SSRF 防护；
- 限流、风控、隐私和滥用检测；
- 独立安全评估。

进一步阅读：

- [核心规范](spec/ASCP-0.2.md)
- [账单 Profile](spec/billing-profile.md)
- [安全 Profile](spec/security-profile.md)
- [部署说明](docs/deployment.md)
- [生产清单](docs/production-readiness-checklist.md)
- [威胁模型](docs/threat-model.md)

## 许可证

Apache License 2.0，见 [LICENSE](LICENSE)。
