# {项目名称} - 项目分析文档

> 🤖 本文档由 AI 自动生成
> 📅 生成时间: {生成时间 YYYY-MM-DD HH:mm}
> 📁 项目路径: `{项目路径}`
> 🧠 模型: {AI 模型名称}

---

## 这是什么项目？

{用 2-3 句话描述项目的核心价值、解决什么问题、面向什么用户。这是整个文档中最重要的一段话，需要让读者在 10 秒内理解项目定位。}

---

## 技术栈

| 类别 | 技术 |
|------|------|
| **语言** | {主要编程语言及版本，如 Python 3.11 / Java 17 / Go 1.21} |
| **框架** | {核心框架，如 FastAPI / Spring Boot 3.x / Gin / React 18} |
| **数据库** | {数据存储，如 PostgreSQL / MySQL / MongoDB / Redis} |
| **其他** | {其他关键技术，如 Celery / RabbitMQ / Docker / Kubernetes} |

---

## 文档导航

| 文档 | 说明 | 适合谁看 |
|------|------|---------|
| 📋 [SUMMARY.md](./SUMMARY.md) | 项目摘要 - 5分钟了解项目全貌 | 所有人 |
| 🏗️ [ARCHITECTURE.md](./ARCHITECTURE.md) | 架构设计 - 系统架构与设计决策 | 架构师、高级开发 |
| 📁 [FILE_INDEX.md](./FILE_INDEX.md) | 文件索引 - 每个文件的职责说明 | 开发者 |

---

## 项目统计

| 指标 | 数值 |
|------|------|
| 📄 总文件数 | {N} 个 |
| 📝 代码行数 | 约 {N} 行 |
| 📦 模块数量 | {N} 个 |
| 🔌 API 端点 | {N} 个 |

---

## 核心模块速览

| 模块 | 路径 | 一句话描述 |
|------|------|-----------|
<!-- FOR module in modules -->
| {模块名称} | `{模块路径}` | {模块职责描述} |
<!-- END FOR -->

---

## 快速开始

### 环境要求

<!-- FOR requirement in requirements -->
- {requirement}
<!-- END FOR -->

### 启动命令

```bash
{启动命令，如: uvicorn main:app --reload}
```

---

## 相关链接

<!-- IF has_links -->
| 链接 | 说明 |
|------|------|
<!-- FOR link in links -->
| [{link.name}]({link.url}) | {link.description} |
<!-- END FOR -->
<!-- END IF -->
