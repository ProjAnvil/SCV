# {Project Name} - File Index

> 🎯 Target Audience: Developers, code reviewers
> 📋 Document Purpose: Quickly locate files, understand each file's responsibility

---

## Icon Legend

| Icon | Meaning |
|------|---------|
| 📁 | Directory |
| 📄 | Regular File |
| ⭐ | Core File (recommended for priority reading) |
| 🔧 | Configuration File |
| 🧪 | Test File |
| 📝 | Documentation File |

---

## Directory Overview

```
{Project Name}/
<!-- FOR item in root_tree -->
{item.prefix} {item.name}
<!-- END FOR -->
```

---

## Root Directory Files

| File | Type | Description |
|------|------|-------------|
<!-- FOR file in root_files -->
| {file.icon} `{file.name}` | {file.type} | {file.description} |
<!-- END FOR -->

---

<!-- FOR directory in directories -->
## 📁 {directory.path}

{directory.description}

| File | Type | Responsibility | Exports |
|------|------|----------------|---------|
<!-- FOR file in directory.files -->
| {file.icon} `{file.name}` | {file.type} | {file.responsibility} | {file.exports} |
<!-- END FOR -->

<!-- IF directory.has_subdirs -->
### Subdirectories

<!-- FOR subdir in directory.subdirs -->
#### 📁 {subdir.path}

{subdir.description}

| File | Type | Responsibility |
|------|------|----------------|
<!-- FOR file in subdir.files -->
| {file.icon} `{file.name}` | {file.type} | {file.responsibility} |
<!-- END FOR -->

<!-- END FOR -->
<!-- END IF -->

---

<!-- END FOR -->

## File Dependency Diagram

```mermaid
graph TD
    subgraph API["API Layer"]
        {API layer file nodes}
    end

    subgraph Service["Service Layer"]
        {Service layer file nodes}
    end

    subgraph Repository["Data Layer"]
        {Data layer file nodes}
    end

    subgraph Core["Core Modules"]
        {Core module file nodes}
    end

    subgraph Model["Data Models"]
        {Data model file nodes}
    end

    {File dependency connection relationships}
```

---

## Quick Location Guide

| I want to... | Should look at |
|--------------|----------------|
<!-- FOR guide in quick_guides -->
| {guide.task} | `{guide.files}` |
<!-- END FOR -->

---

## File Statistics

### By Type

| Type | Count | Percentage |
|------|-------|------------|
<!-- FOR stat in type_stats -->
| {stat.type} | {stat.count} | {stat.percentage} |
<!-- END FOR -->

### By Directory

| Directory | File Count | Lines of Code |
|-----------|------------|---------------|
<!-- FOR stat in dir_stats -->
| `{stat.directory}` | {stat.file_count} | {stat.lines} |
<!-- END FOR -->

---

## Core File Details

Following are the most important files in the project, recommended for priority reading:

<!-- FOR file in core_files -->
### ⭐ `{file.path}`

| Attribute | Description |
|-----------|-------------|
| **Type** | {file.type} |
| **Lines** | {file.lines} |
| **Responsibility** | {file.responsibility} |

**Exports**:

| Name | Type | Description |
|------|------|-------------|
<!-- FOR export in file.exports -->
| `{export.name}` | {export.type} | {export.description} |
<!-- END FOR -->

**Dependencies**:

- Depends on: {file.dependencies}
- Depended by: {file.dependents}

<!-- END FOR -->

---

## Appendix: File Naming Conventions

| Pattern | Description | Example |
|---------|-------------|---------|
<!-- FOR convention in naming_conventions -->
| `{convention.pattern}` | {convention.description} | `{convention.example}` |
<!-- END FOR -->
