# cpa-opencode

<p align="center">
  <img src="assets/logo.svg" alt="OpenCode" width="200" height="200">
</p>

<p align="center">
  <a href="README.md">English</a> · <strong>简体中文</strong>
</p>

<p align="center">
  <a href="#安装">安装</a> ·
  <a href="#配置">配置</a> ·
  <a href="#协议选择">协议选择</a> ·
  <a href="#账户名称与额度">额度与冷却</a> ·
  <a href="https://github.com/dillonzq/cpa-opencode/releases">发布版本</a> ·
  <a href="CONTRIBUTING.md">参与贡献</a>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-555" alt="License: MIT"></a>
  <a href="https://help.router-for.me/plugin/development"><img src="https://img.shields.io/badge/CLIProxyAPI-8.0.0%2B-555" alt="CLIProxyAPI 8.0.0+"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.26.10%2B-555" alt="源码构建需要 Go 1.26.10+"></a>
  <a href="https://github.com/dillonzq/apix/tree/v0.1.0"><img src="https://img.shields.io/badge/apix-0.1.0-555" alt="apix 0.1.0"></a>
</p>

---

用于 [CLIProxyAPI](https://help.router-for.me/plugin/development)（CPA）的 Go 原生动态库插件，以 **`opencode`** 服务商接入 [OpenCode 按量付费模型](https://opencode.ai/v2/docs/console/models/)。

所有模型和客户端协议共用一个凭证池。插件负责模型发现、上游路由和协议转换；CPA 负责凭证选择、轮换、重试、会话亲和与冷却。

插件 ID 为 `cpa-opencode`，服务商 ID、凭证类型及默认模型前缀均为 `opencode`。

## 功能特性

- **统一模型目录**：通过 CPA 的 `/v1/models` 发布 `opencode/<model-id>`，客户端无需分别配置各个模型家族。
- **四种客户端协议**：支持 Chat Completions、Messages、Responses、Gemini 的普通和流式请求；模型支持时优先使用客户端原生协议。
- **工具与推理内容**：使用 [apix v0.1.0](https://github.com/dillonzq/apix/tree/v0.1.0) 转换可表示的媒体、函数工具、工具结果和可读推理；原生请求保留协议专有字段。
- **动态模型发现**：公开目录刷新、models.dev 元数据补全、显式模型声明及逐模型协议和端点覆盖。
- **多凭证调度**：支持独立名称和稳定凭证 ID，由 CPA 在各协议间共享轮换与冷却状态。
- **原生 PAYG quota**：通过 CPA v8 管理接口展示所选凭证的成员预算、余额及可用额度。
- **定向冷却恢复**：充值或故障恢复后，通过 CPA 原生接口恢复目标凭证的调度状态。
- **可取消的上游请求**：客户端断开、超时和插件关闭会取消真实 HTTP 操作，截断流报告错误。

## 环境要求

- **CLIProxyAPI**：`v8.0.0+`，启用原生插件。
- **仅源码构建**：Go `1.26.10+`、启用 CGO，并安装面向 CPA 宿主平台的 C 编译器。使用预编译库无需 Go 工具链。
- **上游凭证**：具有按量付费访问权限的 OpenCode 工作区 API key；查询 quota 还需要对应 Console 读取权限。

## 安装

按 **CPA 实际运行的系统和架构** 选择构建，容器部署时以容器环境为准。可在本仓库 [Actions](https://github.com/dillonzq/cpa-opencode/actions/workflows/build.yml) 或 [Releases](https://github.com/dillonzq/cpa-opencode/releases) 获取构建产物，也可[本地构建](#构建)。

| CPA 宿主系统 | 架构 | 动态库 |
| --- | --- | --- |
| Linux | amd64 / arm64 | `cpa-opencode.so` |
| macOS | amd64 / arm64 | `cpa-opencode.dylib` |
| Windows | amd64 | `cpa-opencode.dll` |

发布包遵循[插件商店要求](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store#release-requirements)，命名为 `cpa-opencode_<version>_<goos>_<goarch>.zip`，版本号不含前导 `v`。每个 ZIP 的根目录包含对应平台的动态库和 `LICENSE`；统一的 `checksums.txt` 列出所有平台 ZIP 的 SHA-256。

1. 对照 `checksums.txt` 验证压缩包的 SHA-256。
2. 解压动态库和 `LICENSE`，将动态库放入 CPA 插件目录，例如 `plugins/linux/amd64/cpa-opencode.so`。
3. 添加下方[最小配置](#最小配置)，并在 CPA 进程环境中设置上游密钥。
4. 重启 CPA，检查插件加载日志。
5. 使用 CPA 客户端密钥查询 `/v1/models`，再请求一个 `opencode/<model-id>` 模型。

## 配置

设置位于 `plugins.configs.cpa-opencode`，全局插件开关和此插件开关均需启用。未知插件字段会报错；环境变量仅在 `credentials[].api-key` 展开。

### 最小配置

```yaml
config-version: 8
plugins:
  enabled: true
  configs:
    cpa-opencode:
      enabled: true
      credentials:
        - name: Personal
          api-key: "${OPENCODE_API_KEY}"
```

在 CPA 进程环境中设置 `OPENCODE_API_KEY`。请求 CPA 使用 **CPA 客户端密钥**；以上 OpenCode 密钥用于上游。未设置的环境变量展开为空值，会导致校验失败。

### 多凭证配置

```yaml
credentials:
  - name: Personal
    api-key: "${OPENCODE_API_KEY}"
  - name: Team
    api-key: "${OPENCODE_TEAM_API_KEY}"
    # workspace-id: org_example
```

每项支持 `name`、`api-key` 和可选 `workspace-id`。当前 API key 已绑定工作区，通常无需额外指定；显式设置后，Console 查询使用 `x-org-id`，推理使用 `x-opencode-org-id`。

### 配置选项

[完整示例](config.example.yaml) 包含目录、元数据、模型前缀和超时设置。

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `base-url` | `https://opencode.ai/inference` | PAYG 基础地址，后面追加协议路径 |
| `catalog-url` | `{base-url}/v1/models` | 公开模型目录 |
| `console-url` | `https://opencode.ai/console` | 成员预算与账单 API |
| `model-prefix.enabled` | `true` | 为公开模型 ID 添加前缀 |
| `model-prefix.value` | `opencode` | 模型前缀 |
| `catalog.refresh-interval` | `15m` | 目录刷新间隔，至少 `1m` |
| `catalog.stale-while-unavailable` | `true` | 目录不可用时保留最后成功快照 |
| `models-dev.enabled` | `true` | 使用 models.dev 补全缺失元数据 |
| `models-dev.url` | `https://models.dev/api.json` | 使用其中的 `opencode` 服务商数据 |
| `models-dev.refresh-interval` | `24h` | 元数据刷新间隔，至少 `1m` |
| `quota.timeout` | `15s` | quota 查询超时，最多 `1m` |
| `request-timeout` | `5m` | 上游请求总超时，包含流式消费 |
| `max-response-bytes` | `67108864` | 普通推理响应大小上限，64 MiB |
| `allow-http` | `false` | 本地测试时显式允许 HTTP |

端点直接追加到配置的基础地址，不删除版本后缀。公开目录刷新不发送密钥，不受凭证失效或冷却影响。模型元数据依次采用用户声明、目录数据，再由可选的 models.dev 数据补全。

## 协议选择

模型家族的默认协议依据 OpenCode [端点表](https://opencode.ai/v2/docs/console/models/)：

| 模型家族 | Chat Completions | Messages | Responses | Gemini |
| --- | :---: | :---: | :---: | :---: |
| `gpt-*`、`grok-*`、`muse-spark-*`、`muse-wonder-*` | — | — | ✓ | — |
| `claude-*`、除 `qwen3.8-max*` 外的 `qwen*` | — | ✓ | — | — |
| `glm-*`、`kimi-*`、`minimax-*`、`mistral-*`、`longcat-*`、`mimo-*`、`step-*`、`beam-*`、`hy*`、`big-pickle`、`qwen3.8-max*` | ✓ | — | — | — |
| `deepseek-*` | ✓ | ✓ | ✓ | — |
| `gemini-*` | — | — | — | ✓ |
| 其他 ID | — | — | — | — |

DeepSeek 同时提供 Messages 和 Responses。未知家族需通过 `models[].protocols` 显式声明；所有协议均关闭的模型不会发布。免费模型即使出现在目录中，也可能要求使用 OpenCode 客户端。

每个请求按客户端格式的顺序，使用模型支持的第一个协议：

| 客户端格式 | 选择顺序 |
| --- | --- |
| Chat Completions | Chat Completions → Responses → Messages → Gemini |
| Responses | Responses → Chat Completions → Messages → Gemini |
| Messages | Messages → Responses → Chat Completions → Gemini |
| Gemini | Gemini → Responses → Chat Completions → Messages |

| 上游协议 | `base-url` 后追加路径 |
| --- | --- |
| Chat Completions | `/openai/v1/chat/completions` |
| Messages | `/anthropic/v1/messages` |
| Responses | `/openai/v1/responses` |
| Gemini | `/google/v1beta/models/{model}:generateContent` |

Gemini 流式改用 `:streamGenerateContent?alt=sse`。所有协议均采用 `Authorization: Bearer <key>` 认证。例如，`opencode/deepseek-v4-pro` 收到 Claude 格式请求时直接使用 Messages，收到 Responses 格式请求时直接使用 Responses。

### 模型与协议覆盖

在插件配置内添加：

```yaml
models:
  - name: custom-model
    display-name: Custom
    max-context-length: 128000
    max-tokens: 8192
    input-modalities: [text, image]
    output-modalities: [text]
    thinking:
      levels: [none, low, medium, high]
      zero-allowed: true
    protocols:
      responses:
        enabled: true
        endpoint: /openai/v1/responses
```

`models[].protocols` 按模型添加、关闭协议并覆盖路径；顶层 `protocols` 仅设置全局路径。列出的协议默认开启，除非 `enabled: false`。Gemini 端点覆盖必须包含 `{model}`。路径优先级为模型覆盖、全局覆盖、内置默认值。

## 请求行为

- 原生请求保留协议专有字段；跨协议转换按目标能力保留文本、图片、函数工具、工具结果和可读推理。不支持的工具类型及无效输入返回明确错误。
- `opencode/glm-5.3(high)` 之类的思考后缀优先于请求体参数，选择推理强度后会从上游模型名中移除。
- DeepSeek 的 `none` 在 Chat Completions 和 Messages 上转换为关闭思考；需要时将图片 `detail: original` 转为 `high`。
- GPT 原生 Responses 保留自身状态；其他 Responses 模型清理外来加密 reasoning，保留可用的可读推理历史。
- 每次推理携带 `x-opencode-session`，依次来自 CPA canonical session、显式会话 header 或初始用户内容的稳定哈希。
- 客户端断开、超时及 quiesce 会取消真实 HTTP 操作；截断流报告失败，已经输出的流不会由插件重放。

## 账户名称与额度

注册时，每个配置密钥生成独立的 CPA 凭证文件。`name` 控制显示名称，基于密钥的稳定 ID 不受更名和排序影响。已有文件和宿主管理的元数据保持保留。移除配置密钥后停止该凭证的执行及模型发布，物理文件可通过 CPA 凭证管理删除。

CPA 在各协议间统一管理凭证选择、轮换与冷却。执行和 quota 查询只使用所选凭证。API key 没有令牌续期流程，`auth.refresh` 校验密钥并保留存储、代理和禁用状态。

### 刷新 quota

管理操作使用 **CPA 管理密钥**。通过 `GET /v8/management/credentials` 取得目标 `auth_index`，再查询最新额度：

```bash
curl -X POST http://localhost:8317/v8/management/credentials/quota/fetch \
  -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"auth_index":"<目标凭证索引>"}'
```

也支持 `GET /v8/management/plugins/cpa-opencode/quota?auth_index=<目标凭证索引>` 和该插件地址的 POST 形式。

| Console 数据源 | CPA 展示内容 |
| --- | --- |
| `/api/policies/current-budgets` | 预算分组、剩余比例和 UTC 重置时间，保留逐模型及共享范围 |
| `/api/billing/status` | 分别展示 USD 余额、可用额度和存在时的信用限额 |

每次刷新重新读取所选密钥。金额由微美分精确换算，余额不转换成没有依据的百分比。读取权限取决于密钥权限和账户角色，服务账户可能无法读取成员预算或账单。一项成功时返回可读数据并记录另一项的脱敏警告；所有数据源均不可读时返回明确错误。成功返回空预算时展示 PAYG 计划。不要求浏览器 Cookie。

### 清除冷却

充值或故障恢复后，恢复目标凭证的本地调度状态：

```bash
curl -X POST http://localhost:8317/v8/management/routing/cooldown/reset \
  -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"auth_index":"<目标凭证索引>"}'
```

额度查询保留推理冷却。清除冷却后，OpenCode 消费和余额保持原值；新的上游 402 或 429 仍会触发冷却。插件不支持服务端额度重置（`supports_reset: false`）。

## 构建

使用 go.mod 指定的 Go 版本，启用 CGO，并安装面向目标平台的 C 编译器：

```bash
# Linux
mkdir -p dist
go build -trimpath -buildmode=c-shared -o dist/cpa-opencode.so .
```

```bash
# macOS
go build -trimpath -buildmode=c-shared -o dist/cpa-opencode.dylib .
```

```powershell
# Windows
go build -trimpath -buildmode=c-shared -o dist/cpa-opencode.dll .
```

版本元数据默认 `0.0.0-dev`。可通过 `-ldflags '-X github.com/dillonzq/cpa-opencode/internal/plugin.pluginVersion=0.1.0'` 注入版本。debug 诊断需要 debug 构建并设置 `CPA_OPENCODE_DEBUG=1`；生产构建不包含该日志器。

## 测试

```bash
go mod download all
go test ./... ./.github/scripts/...
go test -tags debug ./...
go test -tags debug -race ./...
go vet -tags debug ./... ./.github/scripts/...
```

Linux/macOS 的 debug 集成测试通过临时 Go overlay，将生产版动态库加载进 CPA v8。使用本地模拟服务验证推理、流式、取消、逐凭证 quota、402 切换及定向清除冷却。不修改模块缓存；首次运行前通过 `go mod download all` 缓存 SDK 依赖。

真实付费推理和账户余额读取需使用对应凭证单独验收，默认测试不产生付费请求。CI 构建 Linux amd64/arm64、macOS amd64/arm64 和 Windows amd64，每个压缩包包含动态库和许可证。每份 Actions 产物附带该平台的 `checksums.txt`；Release 将其汇总为覆盖全部五个 ZIP 的统一 `checksums.txt`。

## 许可证与图标

[MIT](LICENSE)。OpenCode 图标提供 [SVG](assets/logo.svg) 和 [PNG](assets/logo.png) 版本，品牌资源及插件图标 URL 见[图标说明](assets/README.md)。
