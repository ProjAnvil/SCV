# {项目名称} - 架构设计文档

> 🎯 目标读者: 架构师、技术负责人、高级开发者
> 📋 文档目的: 理解系统架构设计、技术决策、模块关系

---

## 一、架构总览

### 1.1 系统架构图

```mermaid
graph TB
    subgraph Clients["客户端"]
        {客户端节点定义}
    end

    subgraph Gateway["网关层"]
        {网关节点定义}
    end

    subgraph Services["服务层"]
        {服务节点定义}
    end

    subgraph Data["数据层"]
        {数据节点定义}
    end

    {节点连接关系}
```

### 1.2 架构风格

| 属性 | 说明 |
|------|------|
| **架构类型** | {单体应用 / 模块化单体 / 微服务 / Serverless} |
| **通信方式** | {同步REST / 异步消息 / gRPC / GraphQL} |
| **部署模式** | {单机 / 集群 / 容器化 / K8s} |

**架构选型理由**:

{说明为什么选择这种架构风格，考虑了哪些因素}

---

## 二、分层设计

### 2.1 层级架构

```
┌─────────────────────────────────────────────────────────────────┐
│                      Presentation Layer (表现层)                 │
│                                                                 │
│  职责: {表现层职责描述}                                           │
│  组件: {表现层组件列表}                                           │
├─────────────────────────────────────────────────────────────────┤
│                      Application Layer (应用层)                  │
│                                                                 │
│  职责: {应用层职责描述}                                           │
│  组件: {应用层组件列表}                                           │
├─────────────────────────────────────────────────────────────────┤
│                      Domain Layer (领域层)                       │
│                                                                 │
│  职责: {领域层职责描述}                                           │
│  组件: {领域层组件列表}                                           │
├─────────────────────────────────────────────────────────────────┤
│                      Infrastructure Layer (基础设施层)           │
│                                                                 │
│  职责: {基础设施层职责描述}                                        │
│  组件: {基础设施层组件列表}                                        │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 层级依赖规则

```
依赖方向: 上层 → 下层 (单向依赖)

✅ 允许:
   Presentation → Application → Domain → Infrastructure

❌ 禁止:
   - 下层依赖上层
   - 跨层直接依赖 (如 Presentation → Infrastructure)
```

### 2.3 各层详细说明

#### 表现层 (Presentation)

| 组件 | 路径 | 职责 |
|------|------|------|
<!-- FOR component in presentation_components -->
| {component.name} | `{component.path}` | {component.responsibility} |
<!-- END FOR -->

#### 应用层 (Application)

| 组件 | 路径 | 职责 |
|------|------|------|
<!-- FOR component in application_components -->
| {component.name} | `{component.path}` | {component.responsibility} |
<!-- END FOR -->

#### 领域层 (Domain)

| 组件 | 路径 | 职责 |
|------|------|------|
<!-- FOR component in domain_components -->
| {component.name} | `{component.path}` | {component.responsibility} |
<!-- END FOR -->

#### 基础设施层 (Infrastructure)

| 组件 | 路径 | 职责 |
|------|------|------|
<!-- FOR component in infrastructure_components -->
| {component.name} | `{component.path}` | {component.responsibility} |
<!-- END FOR -->

---

## 三、模块设计

### 3.1 模块依赖图

```mermaid
graph LR
    subgraph Core["核心模块"]
        {核心模块节点}
    end

    subgraph Business["业务模块"]
        {业务模块节点}
    end

    subgraph Infrastructure["基础设施"]
        {基础设施节点}
    end

    {模块依赖关系}
```

### 3.2 模块详细说明

<!-- FOR module in modules -->
#### {module.name}

| 属性 | 说明 |
|------|------|
| **路径** | `{module.path}` |
| **职责** | {module.responsibility} |
| **对外接口** | {module.public_api} |
| **依赖模块** | {module.dependencies} |

<!-- END FOR -->

---

## 四、设计模式

### 4.1 使用的设计模式

| 模式 | 应用位置 | 解决的问题 |
|------|---------|-----------|
<!-- FOR pattern in patterns -->
| **{pattern.name}** | `{pattern.location}` | {pattern.problem_solved} |
<!-- END FOR -->

### 4.2 模式实现示例

<!-- FOR pattern in pattern_examples -->
#### {pattern.name}

**问题**: {pattern.problem}

**解决方案**: {pattern.solution}

```{pattern.language}
{pattern.code_example}
```

<!-- END FOR -->

---

## 五、数据流设计

### 5.1 请求处理流程

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant Gateway as 网关
    participant Controller as 控制器
    participant Service as 服务层
    participant Repository as 数据层
    participant DB as 数据库

    {请求处理时序图步骤}
```

### 5.2 核心业务流程

<!-- FOR flow in business_flows -->
#### {flow.name}

```mermaid
{flow.diagram}
```

**流程说明**: {flow.description}

<!-- END FOR -->

---

## 六、外部集成

### 6.1 集成清单

| 服务 | 类型 | 用途 | 集成方式 |
|------|------|------|---------|
<!-- FOR integration in integrations -->
| {integration.name} | {integration.type} | {integration.purpose} | {integration.method} |
<!-- END FOR -->

### 6.2 集成架构图

```
┌─────────────────────────────────────────────────────────────┐
│                        应用服务                              │
├──────────────┬──────────────┬──────────────┬───────────────┤
│              │              │              │               │
▼              ▼              ▼              ▼               ▼
{外部服务连接图}
```

---

## 七、安全架构

### 7.1 认证机制

| 方案 | 实现位置 | 说明 |
|------|---------|------|
<!-- FOR auth in auth_mechanisms -->
| {auth.name} | `{auth.location}` | {auth.description} |
<!-- END FOR -->

### 7.2 授权模型

```
{授权模型描述，如 RBAC / ABAC / ACL}

{权限层级图}
```

### 7.3 安全措施清单

| 威胁 | 防护措施 | 实现位置 |
|------|---------|---------|
<!-- FOR measure in security_measures -->
| {measure.threat} | {measure.protection} | `{measure.location}` |
<!-- END FOR -->

---

## 八、可扩展性设计

### 8.1 水平扩展能力

| 组件 | 扩展方式 | 注意事项 |
|------|---------|---------|
<!-- FOR scale in horizontal_scaling -->
| {scale.component} | {scale.method} | {scale.notes} |
<!-- END FOR -->

### 8.2 垂直拆分预案

```
当前架构 → 未来演进方向:

{拆分预案图}
```

---

## 九、技术决策记录

### 9.1 关键决策

<!-- FOR decision in decisions -->
#### ADR-{decision.id}: {decision.title}

| 属性 | 内容 |
|------|------|
| **状态** | {decision.status} |
| **背景** | {decision.context} |
| **决策** | {decision.decision} |
| **理由** | {decision.rationale} |
| **影响** | {decision.consequences} |

<!-- END FOR -->

---

## 附录

### A. 架构演进历史

| 版本 | 时间 | 变更内容 |
|------|------|---------|
<!-- FOR evolution in architecture_history -->
| {evolution.version} | {evolution.date} | {evolution.changes} |
<!-- END FOR -->

### B. 参考资料

<!-- FOR ref in references -->
- [{ref.title}]({ref.url})
<!-- END FOR -->
