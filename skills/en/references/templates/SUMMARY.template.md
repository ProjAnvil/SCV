# {Project Name} - Project Summary

> ⏱️ Reading Time: ~5 minutes
> 🎯 Target Audience: Developers, product managers, new members who need quick project overview

---

## 1. Project Positioning

### 1.1 One-Sentence Description

**{Project Name}** is a **{Project Type}** for **{Core Function}**, targeting **{Target Users}**.

### 1.2 Core Feature List

| Status | Feature | Description |
|--------|---------|-------------|
<!-- FOR feature in features -->
| {feature.status} | {feature.name} | {feature.description} |
<!-- END FOR -->

> Status Legend: ✅ Completed | 🚧 In Development | 📋 Planned

### 1.3 Project Type

**{Project Type}**: {Type Description}

> Available Types: Web Application / RESTful API / Microservices / CLI Tool / SDK / Data Pipeline / Desktop Application / Mobile Application

---

## 2. Technology Choices

### 2.1 Technology Stack Panorama

```
┌─────────────────────────────────────────────────────────────┐
│                      Client Layer                         │
│  {Frontend stack, e.g., React 18 + TypeScript + Ant Design} │
├─────────────────────────────────────────────────────────────┤
│                      Server Layer                         │
│  {Backend stack, e.g., FastAPI + SQLAlchemy + Pydantic}     │
├─────────────────────────────────────────────────────────────┤
│                        Data Layer                         │
│  {Data stack, e.g., PostgreSQL + Redis + Elasticsearch}    │
├─────────────────────────────────────────────────────────────┤
│                     Infrastructure Layer                  │
│  {Infrastructure, e.g., Docker + Nginx + GitHub Actions} │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 Core Dependencies List

| Dependency | Version | Purpose |
|------------|---------|---------|
<!-- FOR dep in dependencies -->
| `{dep.name}` | {dep.version} | {dep.purpose} |
<!-- END FOR -->

---

## 3. Directory Structure

```
{Project Root}/
<!-- FOR item in directory_tree -->
{item.indent}{item.icon} {item.name}/          # {item.description}
<!-- END FOR -->
```

### Directory Responsibility Description

| Directory | Layer | Responsibility |
|-----------|-------|---------------|
<!-- FOR dir in directories -->
| `{dir.path}` | {dir.layer} | {dir.responsibility} |
<!-- END FOR -->

---

## 4. Module Division

### 4.1 Business Modules

| Module | Path | Responsibility | Key Files |
|--------|------|----------------|-----------|
<!-- FOR module in business_modules -->
| **{module.name}** | `{module.path}` | {module.responsibility} | {module.key_files} |
<!-- END FOR -->

### 4.2 Infrastructure Modules

| Module | Path | Responsibility |
|--------|------|----------------|
<!-- FOR module in infra_modules -->
| **{module.name}** | `{module.path}` | {module.responsibility} |
<!-- END FOR -->

---

## 5. Data Models

### 5.1 Core Entity Relationships

```
{Entity relationship diagram, using ASCII or simple description}

Example:
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

### 5.2 Entity Description

| Entity | Table Name | Description | Key Fields |
|--------|------------|-------------|------------|
<!-- FOR entity in entities -->
| {entity.name} | `{entity.table}` | {entity.description} | {entity.key_fields} |
<!-- END FOR -->

---

## 6. API Overview

### 6.1 Endpoint Statistics

| Module | Endpoint Count | Auth Requirement | Base Path |
|--------|---------------|------------------|-----------|
<!-- FOR api_group in api_groups -->
| {api_group.name} | {api_group.count} | {api_group.auth} | `{api_group.base_path}` |
<!-- END FOR -->

### 6.2 Core Endpoint List

| Method | Path | Description | Auth |
|--------|------|-------------|------|
<!-- FOR endpoint in key_endpoints -->
| `{endpoint.method}` | `{endpoint.path}` | {endpoint.description} | {endpoint.auth} |
<!-- END FOR -->

---

## 7. Configuration Notes

### 7.1 Environment Variables

| Variable Name | Required | Description | Example Value |
|---------------|----------|-------------|---------------|
<!-- FOR env in env_vars -->
| `{env.name}` | {env.required} | {env.description} | `{env.example}` |
<!-- END FOR -->

### 7.2 Configuration File List

| File | Purpose | Tracked by Git |
|------|---------|---------------|
<!-- FOR config in config_files -->
| `{config.path}` | {config.purpose} | {config.git_tracked} |
<!-- END FOR -->

---

## 8. Important Notes

### 8.1 Technical Highlights ✨

<!-- FOR highlight in highlights -->
- **{highlight.title}**: {highlight.description}
<!-- END FOR -->

### 8.2 Known Issues ⚠️

<!-- FOR issue in known_issues -->
- {issue}
<!-- END FOR -->

### 8.3 Improvement Suggestions 💡

<!-- FOR suggestion in suggestions -->
- {suggestion}
<!-- END FOR -->

---

## Appendix: Glossary

| Term | Description |
|------|-------------|
<!-- FOR term in glossary -->
| {term.name} | {term.definition} |
<!-- END FOR -->
