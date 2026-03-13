# {项目名称} - 项目摘要

> ⏱️ 阅读时间: 约 5 分钟
> 🎯 目标读者: 需要快速了解项目全貌的开发者、产品经理、新成员

---

## 一、项目定位

### 1.1 一句话描述

**{项目名称}** 是一个 **{项目类型}**，用于 **{核心功能}**，面向 **{目标用户}**。

### 1.2 核心功能清单

| 状态 | 功能 | 说明 |
|------|------|------|
<!-- FOR feature in features -->
| {feature.status} | {feature.name} | {feature.description} |
<!-- END FOR -->

> 状态说明: ✅ 已完成 | 🚧 开发中 | 📋 规划中

### 1.3 项目类型

**{项目类型}**: {类型说明}

> 可选类型: Web应用 / RESTful API / 微服务 / CLI工具 / SDK / 数据管道 / 桌面应用 / 移动应用

---

## 二、技术选型

### 2.1 技术栈全景

```
┌─────────────────────────────────────────────────────────────┐
│                        客户端层                              │
│  {前端技术栈，如: React 18 + TypeScript + Ant Design}        │
├─────────────────────────────────────────────────────────────┤
│                        服务端层                              │
│  {后端技术栈，如: FastAPI + SQLAlchemy + Pydantic}          │
├─────────────────────────────────────────────────────────────┤
│                        数据层                                │
│  {数据技术栈，如: PostgreSQL + Redis + Elasticsearch}       │
├─────────────────────────────────────────────────────────────┤
│                        基础设施层                            │
│  {基础设施，如: Docker + Nginx + GitHub Actions}            │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 核心依赖清单

| 依赖 | 版本 | 用途 |
|------|------|------|
<!-- FOR dep in dependencies -->
| `{dep.name}` | {dep.version} | {dep.purpose} |
<!-- END FOR -->

---

## 三、目录结构

```
{项目根目录}/
<!-- FOR item in directory_tree -->
{item.indent}{item.icon} {item.name}/          # {item.description}
<!-- END FOR -->
```

### 目录职责说明

| 目录 | 层级 | 职责 |
|------|------|------|
<!-- FOR dir in directories -->
| `{dir.path}` | {dir.layer} | {dir.responsibility} |
<!-- END FOR -->

---

## 四、模块划分

### 4.1 业务模块

| 模块 | 路径 | 职责 | 核心文件 |
|------|------|------|---------|
<!-- FOR module in business_modules -->
| **{module.name}** | `{module.path}` | {module.responsibility} | {module.key_files} |
<!-- END FOR -->

### 4.2 基础模块

| 模块 | 路径 | 职责 |
|------|------|------|
<!-- FOR module in infra_modules -->
| **{module.name}** | `{module.path}` | {module.responsibility} |
<!-- END FOR -->

---

## 五、数据模型

### 5.1 核心实体关系

```
{实体关系图，使用 ASCII 或简单描述}

示例:
┌──────────┐     ┌──────────┐     ┌──────────┐
│   User   │────<│  Order   │────<│ OrderItem│
└──────────┘     └──────────┘     └──────────┘
     │                                  │
     └──────────────┬──────────────────┘
                    ▼
              ┌──────────┐
              │ Product  │
              └──────────┘
```

### 5.2 实体说明

| 实体 | 表名 | 说明 | 核心字段 |
|------|------|------|---------|
<!-- FOR entity in entities -->
| {entity.name} | `{entity.table}` | {entity.description} | {entity.key_fields} |
<!-- END FOR -->

---

## 六、API 概览

### 6.1 端点统计

| 模块 | 端点数 | 认证要求 | 基础路径 |
|------|--------|---------|---------|
<!-- FOR api_group in api_groups -->
| {api_group.name} | {api_group.count} | {api_group.auth} | `{api_group.base_path}` |
<!-- END FOR -->

### 6.2 核心端点清单

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
<!-- FOR endpoint in key_endpoints -->
| `{endpoint.method}` | `{endpoint.path}` | {endpoint.description} | {endpoint.auth} |
<!-- END FOR -->

---

## 七、配置说明

### 7.1 环境变量

| 变量名 | 必填 | 说明 | 示例值 |
|--------|------|------|--------|
<!-- FOR env in env_vars -->
| `{env.name}` | {env.required} | {env.description} | `{env.example}` |
<!-- END FOR -->

### 7.2 配置文件清单

| 文件 | 用途 | 是否提交Git |
|------|------|------------|
<!-- FOR config in config_files -->
| `{config.path}` | {config.purpose} | {config.git_tracked} |
<!-- END FOR -->

---

## 八、注意事项

### 8.1 技术亮点 ✨

<!-- FOR highlight in highlights -->
- **{highlight.title}**: {highlight.description}
<!-- END FOR -->

### 8.2 已知问题 ⚠️

<!-- FOR issue in known_issues -->
- {issue}
<!-- END FOR -->

### 8.3 改进建议 💡

<!-- FOR suggestion in suggestions -->
- {suggestion}
<!-- END FOR -->

---

## 附录：术语表

| 术语 | 说明 |
|------|------|
<!-- FOR term in glossary -->
| {term.name} | {term.definition} |
<!-- END FOR -->
