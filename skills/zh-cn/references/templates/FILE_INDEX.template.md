# {项目名称} - 文件索引

> 🎯 目标读者: 开发者、代码审查者
> 📋 文档目的: 快速定位文件、理解每个文件的职责

---

## 图标说明

| 图标 | 含义 |
|------|------|
| 📁 | 目录 |
| 📄 | 普通文件 |
| ⭐ | 核心文件 (建议优先阅读) |
| 🔧 | 配置文件 |
| 🧪 | 测试文件 |
| 📝 | 文档文件 |

---

## 目录总览

```
{项目名称}/
<!-- FOR item in root_tree -->
{item.prefix} {item.name}
<!-- END FOR -->
```

---

## 根目录文件

| 文件 | 类型 | 说明 |
|------|------|------|
<!-- FOR file in root_files -->
| {file.icon} `{file.name}` | {file.type} | {file.description} |
<!-- END FOR -->

---

<!-- FOR directory in directories -->
## 📁 {directory.path}

{directory.description}

| 文件 | 类型 | 职责 | 导出内容 |
|------|------|------|---------|
<!-- FOR file in directory.files -->
| {file.icon} `{file.name}` | {file.type} | {file.responsibility} | {file.exports} |
<!-- END FOR -->

<!-- IF directory.has_subdirs -->
### 子目录

<!-- FOR subdir in directory.subdirs -->
#### 📁 {subdir.path}

{subdir.description}

| 文件 | 类型 | 职责 |
|------|------|------|
<!-- FOR file in subdir.files -->
| {file.icon} `{file.name}` | {file.type} | {file.responsibility} |
<!-- END FOR -->

<!-- END FOR -->
<!-- END IF -->

---

<!-- END FOR -->

## 文件依赖关系图

```mermaid
graph TD
    subgraph API["API 层"]
        {API层文件节点}
    end

    subgraph Service["服务层"]
        {服务层文件节点}
    end

    subgraph Repository["数据层"]
        {数据层文件节点}
    end

    subgraph Core["核心模块"]
        {核心模块文件节点}
    end

    subgraph Model["数据模型"]
        {数据模型文件节点}
    end

    {文件依赖连接关系}
```

---

## 快速定位指南

| 我想要... | 应该查看 |
|-----------|---------|
<!-- FOR guide in quick_guides -->
| {guide.task} | `{guide.files}` |
<!-- END FOR -->

---

## 文件统计

### 按类型统计

| 类型 | 数量 | 占比 |
|------|------|------|
<!-- FOR stat in type_stats -->
| {stat.type} | {stat.count} | {stat.percentage} |
<!-- END FOR -->

### 按目录统计

| 目录 | 文件数 | 代码行数 |
|------|--------|---------|
<!-- FOR stat in dir_stats -->
| `{stat.directory}` | {stat.file_count} | {stat.lines} |
<!-- END FOR -->

---

## 核心文件详解

以下是项目中最重要的文件，建议优先阅读：

<!-- FOR file in core_files -->
### ⭐ `{file.path}`

| 属性 | 说明 |
|------|------|
| **类型** | {file.type} |
| **行数** | {file.lines} |
| **职责** | {file.responsibility} |

**导出内容**:

| 名称 | 类型 | 说明 |
|------|------|------|
<!-- FOR export in file.exports -->
| `{export.name}` | {export.type} | {export.description} |
<!-- END FOR -->

**依赖关系**:

- 依赖: {file.dependencies}
- 被依赖: {file.dependents}

<!-- END FOR -->

---

## 附录：文件命名规范

| 模式 | 说明 | 示例 |
|------|------|------|
<!-- FOR convention in naming_conventions -->
| `{convention.pattern}` | {convention.description} | `{convention.example}` |
<!-- END FOR -->
