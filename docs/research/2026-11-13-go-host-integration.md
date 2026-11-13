# SCV Go 宿主集成调研报告

- 日期：2026-11-13
- 调研基线：main @ b575c10（SCV v0.13.0）
- 参与角色：system-architect（架构调研）、product-manager（产品需求）
- 状态：调研完成，待作者拍板（见第 10 节）

---

## 1. 结论摘要

**可行，且比预想的近。** SCV 的机器可调用面已经高度 CLI 化、JSON 化，查询层已经是纯 Go：

1. `perception/perceive.py` 是单行 JSON 契约的结构分析编排器（exit 0 恒定，失败语义编码在 `status/reason`），Go 直接 exec 即可。
2. `perception/graphmcp/` 已是一个 **Go 实现的 MCP server**（stdio + Streamable HTTP 双 transport，5 个查询工具），底层 `github.com/projanvil/vectra v0.14.4` 是纯 Go 库，**Go 宿主可以直接 import，零进程开销**。
3. 依赖 LLM 的只有 4 份分析文档（README/SUMMARY/ARCHITECTURE/FILE_INDEX）的正文生成。结构分析、增量跳过、批处理状态机全部无 LLM。
4. **推荐方案：薄 CLI 编排（Go exec perceive.py + 轮询 perception.log/sessions 状态文件）+ 查询层以 vectra 库形式内嵌。不建 Python sidecar。** MVP 约 3.5–5 人周。

## 2. 两个需要修正的背景假设

| 原假设 | 实际情况 | 佐证 |
|---|---|---|
| Spring Boot 探查需要 JAVA_HOME | **默认运行路径已不需要 JDK**。v0.11.0 起 Spring Boot 组件抽取由纯 Python tree-sitter 扩展实现；Spoon（Java/Gradle）仅保留为开发期交叉验证引擎，不随 install.sh 发布、不在运行路径上。 | `CHANGELOG.md` v0.11.0；`perception/DEVELOPERS.md`；`perception/analyzer/extensions/springboot.py` |
| "UV home" 需要配置 | perceive.py 只用 `shutil.which("uv")` 找 uv 可执行文件，uv 自管 venv 与缓存（`uv run --project analyzer`，uv.lock 已提交）。**没有 UV_HOME 这个概念**。Go 侧需要的是"发现 uv 在哪"，不是配置 UV_HOME。 | `perception/perceive.py:267-292` |
| （附带）vectra 是向量索引 | vectra 是**本地图存储/查询引擎**（Cypher 子集 + FTS + .vg 快照），不是 embedding 索引。embedding 层在规格文档中被显式推迟。 | `perception/loader/main.go`、v3 design spec |

含义：环境发现接口仍按需求暴露 uv / JAVA_HOME（后者标注可选、供 dev 侧 crosscheck 或未来 probe 用），但文档需写明 Java 非必需。

## 3. 现状盘点：SCV 四层架构

| 层 | 位置 | 性质 |
|---|---|---|
| 技能编排层 | `skills/{en,zh-cn}/SKILL.md` + `references/{run,batchRun,gather,index}.md` | **LLM 驱动**：`/scv run` 的 8 个步骤由 Claude 执行 bash |
| 确定性脚本层 | `skills/scripts/{git_op.py, scv_util.py, batch_manager.py}` | 纯 Python CLI，无 LLM。批处理状态机落盘 `~/.scv/sessions/{id}.json` |
| 文档生成层 | `agents/*/project-analyzer.md` + `references/templates/` | **LLM 驱动**：4 份文档正文由 subagent 按模板撰写 |
| 结构感知层 | `perception/`：perceive.py（编排，系统 python3.9+ 即可）→ analyzer（uv + tree-sitter 六语言）→ loader（Go，structure.json→structure.vg+环分级）→ graphmcp（Go MCP server） | 确定性程序 |

关键契约：

- **perceive.py**：`perceive.py <repo> <analysis_dir> --source local|remote --lang zh|en`，stdout 单行 JSON（status/reason/tiers/counts/artifacts/structure_summary/mcp_add/footnote），exit 0 恒定。环境变量钩子：`SCV_PERCEPTION_DIST`、`SCV_PERCEPTION_CONFIG`、`SCV_PERCEPTION_ANALYZER`。tier 流水线：tier0 配置门 → tier1 检测 → tier2 uv → tier3 analyzer → tier6 loader → tier8 graphmcp，每阶段写 `{analysis_dir}/perception.log`。超时 600s 硬编码。
- **graphmcp**（`perception/graphmcp/main.go`，go 1.27.2，MCP go-sdk v1.8.0）：工具 `repos` / `cypher`（含 FTS CONTAINS）/ `get_node` / `impact`（vectra Reachable 传递影响面）/ `refresh`。数据源 `{analysis-dir}/*/structure.vg`，懒加载缓存。
- **状态落盘**：`~/.scv/sessions/{id}.json`（批处理状态机：repos[].status、batches、current_batch_num）、每仓 `.scv_metadata.json`（last_analyzed_commit，增量跳过依据）。
- **部署布局**（install.sh）：`~/.scv/{repos,analysis,sessions,config.json}` + `~/.scv/perception/`（perceive.py + loader/ + graphmcp/ + analyzer/ 含 uv.lock）。

## 4. 能力边界：机器可调 vs 依赖 LLM

**Go 宿主不带 LLM 可完整获得**：
- 多语言结构图 structure.json（File/Type/Method/Bean 节点 + CONTAINS/IMPLEMENTS/EXTENDS/CALLS/REFERENCES/IMPORTS/INJECTS 边）
- 可查询快照 structure.vg + 循环依赖分级（fatal/tolerated）
- 图查询（Cypher/FTS/影响面，vectra 库或 graphmcp）
- 编排与状态（增量跳过、批处理状态机，全部磁盘 JSON）

**依赖 Claude LLM，Go 宿主天然缺失**：
- 4 份文档正文（project-analyzer subagent 按模板撰写；subagent 提示词 `agents/*/project-analyzer.md` 可移植给宿主自有 LLM）
- `~/.scv/index.md` 摘要（明确禁止脚本解析，须 LLM 阅读）

`structure_summary` 文案与循环计数是 perceive.py 预渲染的，不需要 LLM。

## 5. 集成方案对比与推荐

| 维度 | (a) Python sidecar + HTTP/gRPC | (b) 扩展 graphmcp，Go 做 MCP client | (c) 薄 CLI 编排 + vectra 库内嵌 ✅ |
|---|---|---|---|
| 打包 | 新增 Python 服务，宿主管 sidecar 生命周期 | graphmcp 已是 Go 二进制 | 复用已装 `~/.scv/perception`，uv 自管 |
| 进程管理 | 守护进程全套自建 | 查询侧简单，分析侧仍需另起 | 无常驻进程，Go ctx 取消即 kill |
| 进度推送 | 最强（SSE/WS），但 perceive.py 不输出实时进度，**兑现仍需改 Python**，违背"不重写"约束 | MCP tool 同步语义，长任务需 job_id+轮询间接层，别扭 | 轮询 perception.log tier 行（秒级）+ 完成时 stdout JSON；阶段粒度够 MVP |
| 对现有代码侵入 | 高 | 中（graphmcp 加 tools） | **零** |
| 运维复杂度 | 最高 | 中 | 最低 |

**推荐 (c)，吸收 (b) 的查询能力**：查询层本就是 100% Go（vectra `LoadFile` → `Cypher/NodeByKey/Reachable`），Go 宿主直接 import；graphmcp 保留为可选共享 MCP 服务（对 LLM 工具链友好）。

### 组件设计草图

```
┌──────────────────────── Go 宿主 (Fiber app) ────────────────────────┐
│  POST /api/scv/analyze         提交分析 job（repo 路径/URL + opts）  │
│  GET  /api/scv/jobs/:id        job 状态（tier 进度 + perceive JSON） │
│  GET  /api/scv/jobs/:id/events SSE 进度（Fiber 原生）                │
│  GET  /api/scv/repos           已分析仓列表                          │
│  POST /api/scv/query           cypher / impact / get_node           │
│  GET  /api/scv/env             环境发现（uv/java/go/scv home/扩展）  │
│                                                                      │
│  scvsdk 包（独立 module，非 Fiber 宿主也可用）:                       │
│   Env    ResolveEnv()        配置发现（见下表）                      │
│   Runner Analyze(ctx, ...)   exec python3 perceive.py（env 注入）    │
│          · goroutine tail perception.log → tier 事件 → onProgress   │
│          · EnsureDist() 预构建，规避并发 go build 竞争               │
│   Graph  vectra.LoadFile(.vg) → Cypher/Node/Impact 薄封装           │
│   Meta   .scv_metadata.json 读写（增量跳过，复刻 scv_util 语义）     │
└──────────────────────────────────────────────────────────────────────┘
            │ exec + 文件                        │ import
            ▼                                    ▼
  perception/perceive.py（原样）      github.com/projanvil/vectra v0.14.4
```

**Python 侧新增服务边界：无。** 唯一建议的小改动（非 MVP 阻塞）：给 perceive.py 加 `--version` 输出契约版本，供 SDK 版本协商。

### 配置发现契约（Env.ResolveEnv 解析顺序）

| 项 | 顺序 |
|---|---|
| SCV home | ① env `SCV_HOME` ② `$HOME/.scv` |
| perception 根 | ① env `SCV_PERCEPTION_ROOT` ② `$SCV_HOME/perception` ③ 内嵌分发 |
| dist 目录 | env `SCV_PERCEPTION_DIST` 透传（默认 `$SCV_HOME/perception/dist`） |
| uv | ① env `SCV_UV` ② `LookPath("uv")` ③ `~/.local/bin/uv`、`/opt/homebrew/bin/uv`。发现失败不阻断，标记 diagnostic（分析时以 uv-missing 优雅降级） |
| python3（编排层） | `LookPath("python3")`，3.9+ 即可（实测）；analyzer 的 Python 3.11+ 由 uv 保证 |
| JAVA_HOME | ① env ② `/usr/libexec/java_home`（macOS）③ sdkman ④ JVM 目录扫描。**仅信息性暴露**（当前运行链不用 Java） |
| Go（可选查询 tier） | `go version` >= 1.27 |

## 6. Go SDK API 草案（产品契约）

```go
package scv

// 初始化与环境
func New(ctx context.Context, cfg Config) (*Client, error)      // 幂等建 ~/.scv，不覆盖用户 config
func (c *Client) DiscoverEnv(ctx) (*Environment, error)          // uv/go/java(可选)/扩展/语言/codebones
func (c *Client) Capabilities(ctx) (*Capabilities, error)        // 版本协商

// 仓库管理（gather 等价）
func (c *Client) Clone(ctx, url, branch) (*Repo, error)
func (c *Client) ListRepos(ctx) ([]Repo, error)

// 分析 workflow（核心）
type RunRequest struct {
    Source        string // 本地路径或 Git URL
    ProjectName   string // 缺省 basename
    OutputDir     string // 指定文档生成位置；缺省 ~/.scv/analysis/{repo}
    SkipUnchanged bool   // commit 未变 → skipped 终态
}
func (c *Client) Run(ctx, req RunRequest) (Workflow, error)

type Workflow interface {
    ID() string
    Status(ctx) (*WorkflowStatus, error)   // 轮询
    Events() <-chan WorkflowEvent          // 订阅（进程内 channel，宿主自行桥 SSE/WS）
    Cancel(ctx) error                      // ctx 取消 → kill 进程树 → cancelled 终态
    Wait(ctx) (*WorkflowResult, error)
}
func (c *Client) FindOutput(ctx, project) (string, error)   // 产物位置查询
func (c *Client) GetDocs(ctx, project) (*Docs, error)       // 4 文档存在性（headless 模式下有正文）

// 结构图查询（vectra 内嵌，或可选走 graphmcp MCP/HTTP）
func (c *Client) Graph() GraphQuery   // Repos / Cypher / GetNode / Impact / Refresh
```

### Workflow 状态模型

```go
pending → preparing → detecting → analyzing → grading → query_tier → generating → finalizing → done
                                                                                      ↘ failed / cancelled / skipped
```

- 阶段一一映射现有执行单元：preparing=run.md Step1-4（clone/commit 信息）、detecting=tier0/1、analyzing=tier2/3、grading=tier6（loader+环分级）、query_tier=tier8、generating=Step6（LLM 文档；MVP structure-only 时跳过）、finalizing=Step7（metadata/index）。
- **降级不失败**（延续 perceive.py 契约）：go-missing/mcp-missing 产生 `degraded` 事件但继续到 done；not-framework 是信息性结果非失败。
- **进度粒度：阶段级**。analyzer 是黑盒子子进程，现有链路无逐文件输出，硬造百分比必然是假的。`Percent` 保留为可选估算字段（标注 estimated）。
- 事件模型：`stage_started/stage_finished/stage_failed/degraded/artifact_produced/workflow_finished`，Seq 单调递增，错误带 LogTail（对齐 perception.log tail ≤2000 字符）。

### 错误码（映射现有来源）

`ENV_UV_MISSING`、`ANALYZER_FAILED`、`CLONE_FAILED`、`PATH_NOT_FOUND`、`STAGE_TIMEOUT`（600s）、`STRUCTURE_DISABLED`、`DOCGEN_UNAVAILABLE`、`VERSION_INCOMPATIBLE` 等；降级类（`ENV_GO_MISSING`/`ENV_MCP_MISSING`）走事件不走错误。未知 reason 原样透传不炸（前向兼容）。

### 版本协商

三条版本轴：SCV 发布版（CHANGELOG）、perceive JSON 契约（**当前无版本字段**——建议加 `--version`，SDK 宽松解析兜底）、vectra 快照 store 版本。`.scv_metadata.json` 双写兼容（键只增不改），保证 Claude Code 宿主与 Go 宿主读写同一份增量状态互不破坏。

## 7. MVP 范围与验收（摘要）

**MVP = 环境发现 + 单仓 structure-only 分析 workflow（状态/事件/取消/指定输出目录）+ 结构图查询 + 错误与版本协商。** headless 文档正文生成不在 MVP（接口预留 DocGenHeadless）。

关键验收（完整 12 条见 PRD）：
- AC-4：本地 Git 仓库 Run(OutputDir 指定) → 依次收到 preparing→…→grading 事件，指定目录产出 structure.json/.vg/perception.log，终态 done
- AC-6：同 commit 重跑 SkipUnchanged → 终态 skipped
- AC-7：Cancel → 进程树终止、cancelled 终态、重复 Cancel 无副作用
- AC-8：Graph().Cypher 计数与 structure.json 一致；Impact 不含起始节点
- AC-12：全程不执行被分析仓库内任何可执行文件（安全不变量）

测试 fixture 直接复用 `perception/fixture/` 的多语言固定仓库。

## 8. 风险与工作量

| 风险 | 缓解 |
|---|---|
| perceive stdout JSON 无版本字段 | 宽松解析 + fixture 集成测试钉住 |
| 首次并发多仓触发多个 go build 竞争同一 dist | 宿主 EnsureDist() 串行预构建 + 文件锁 |
| uv 首跑需网络（下 Python/依赖） | 离线部署预热 uv cache；启动诊断提示 |
| 同仓并发重分析冲突（cleanup_stale 先删产物） | job 级同 repo 互斥或独立 analysis_dir |
| 文档层缺口（无 LLM 无正文） | 产品边界显式声明；宿主按 project-analyzer.md 提示词接自有 LLM |

工作量：scvsdk（Env+Runner+JSON+日志 tail）1.5–2 人周；查询层 0.5–1；Fiber component（job API+SSE+并发控制）1；测试+文档 0.5–1。**合计 3.5–5 人周**。对比 sidecar 4–6 且长期运维成本最高。

## 9. 已否决方案

- **(a) Python sidecar**：多一层跨语言契约与守护进程运维，唯一优势（实时流）需改 Python 层才能兑现，违背"不重写"约束。
- **(b) 纯 MCP client**：分钟级编排塞进同步 tool 语义需 job 轮询间接层；查询走 MCP 比走库多一跳。graphmcp 保留为可选共享服务，不做主接入层。

## 10. 待拍板问题（按优先级）

1. **无 LLM 宿主的文档正文路线**（阻塞 US-10 排期）：structure-only 先交付 + 宿主自产？还是 SDK 预留 headless 接口（外部 LLM 命令行模板，提示词/模板仓库内现成）？
2. **进度推送形态**：进程内 channel（轮询+订阅双 API）已够，还是要 SDK 内置跨进程 SSE/HTTP 流？
3. **进程模型**：按次 fork perceive.py（贴现状）vs 常驻 daemon（省 uv 首启约半分钟、利并发控制）？
4. **向量检索语义**：确认即指 vectra 结构图查询；若要 embedding 语义检索属新立项（规格已推迟）。
5. **Go workflow 状态是否复用 `~/.scv/sessions/` 格式**（复用则 Claude Code 宿主可 resume Go 发起的批次）。
6. **JAVA_HOME 定位**：信息性字段即可，还是要为未来 probe/Spoon 复活预留可执行探查？
7. Go 工具链下限：SDK 整体钉 1.27（对齐 repo go.mod），还是查询 tier 可选、核心放宽？

---

## 11. 多宿主分发路线（2026-11-13 补充，与作者对齐后）

路线 = **一份 runtime 包 + 一套冻结契约 + 每个宿主一个薄 SDK**：

- **第一层，SCV Runtime 包（唯一部署单元，与宿主语言无关）**：`perception-runtime-<ver>.tar.gz` = perceive.py + analyzer（uv 项目 + uv.lock，平台无关源码）+ `dist/{linux,darwin}-{amd64,arm64}/loader,scv-graph-mcp`（预编译 Go 二进制，宿主无需 Go 工具链）。不含 `.venv`（uv 首跑自建）、不含 adapters 源码。分发方式三选一：复用 install.sh / release tarball（推荐）/ go:embed 物化。
- **第二层，冻结契约（所有 SDK 包同一个东西）**：perceive.py stdout 单行 JSON、perception.log tier 行、`~/.scv/sessions/*.json` 状态、structure.json/.vg 产物、config.json + env 钩子（`SCV_PERCEPTION_ROOT/DIST/CONFIG`）。
- **第三层，宿主 SDK（薄适配）**：**触发面不分叉**（一律 exec perceive.py）；**查询面按宿主分叉**——Go 直接 import vectra（零额外进程），其他生态走 graphmcp 的 Streamable HTTP（MCP 即通用查询 API）。每加一个宿主语言只多一层 exec + JSON 解析 + MCP client，无分析逻辑 N 份拷贝。

SCV 侧待办三件：`make pack` 打包脚本；perceive.py 加 `--version`（唯一 Python 改动）；契约文档化。Go SDK（scvsdk + Fiber component）3.5–5 人周，第一个消费方。未拍板项（headless 文档正文、进度推送形态）不阻塞本路线。

### Java 探测的决策矩阵（对齐结论）

Spring Boot 运行时提取默认永远是 tree-sitter（v0.11.0 决策：部署门槛、安全——Spoon GRADLE 模式需解析目标仓构建文件、精度已验证打平、多语言统一；见 `docs/superpowers/specs/2026-10-14-perception-v2-treesitter-design.md`）。若未来复活 Spoon 深度档：

| 条件 | 引擎 |
|---|---|
| JDK ✓ + 本地受信仓 | Spoon GRADLE 模式（语义精确） |
| JDK ✓ + 远程仓 | tree-sitter（安全策略禁止执行目标仓构建） |
| JDK ✗ | tree-sitter |

noclasspath 不在选项里（degraded、false mismatches，`perception/DEVELOPERS.md:198`）。`DiscoverEnv()` 保留 JAVA_HOME 信息性探测（env → `/usr/libexec/java_home` → sdkman → JVM 目录扫描），即为该深度档预留。

## 附：关键文件索引

| 主题 | 文件 |
|---|---|
| 编排契约（单行 JSON、tier 流水线） | `perception/perceive.py` |
| Go MCP 查询服务（5 工具、双 transport） | `perception/graphmcp/main.go` |
| 快照转换（structure.json→.vg+环分级） | `perception/loader/main.go` |
| tree-sitter 分析器（uv 项目） | `perception/analyzer/analyzer.py`、`perception/analyzer/pyproject.toml` |
| 技能指挥链（run 8 步、batchRun 状态机约束） | `skills/en/SKILL.md`、`skills/en/references/run.md`、`skills/en/references/batchRun.md` |
| 纯脚本层 | `skills/scripts/{git_op,scv_util,batch_manager}.py` |
| 文档生成提示词与模板 | `agents/en/project-analyzer.md`、`skills/en/references/templates/` |
| 部署布局 | `install.sh` |
| 架构契约文档 | `perception/DEVELOPERS.md`、`perception/README.md` |
