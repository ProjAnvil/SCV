# {Project Name} - Architecture Design Document

> 🎯 Target Audience: Architects, technical leads, senior developers
> 📋 Document Purpose: Understand system architecture design, technical decisions, module relationships

---

## 1. Architecture Overview

### 1.1 System Architecture Diagram

```mermaid
graph TB
    subgraph Clients["Clients"]
        {Client node definitions}
    end

    subgraph Gateway["Gateway Layer"]
        {Gateway node definitions}
    end

    subgraph Services["Service Layer"]
        {Service node definitions}
    end

    subgraph Data["Data Layer"]
        {Data node definitions}
    end

    {Node connection relationships}
```

### 1.2 Architecture Style

| Attribute | Description |
|-----------|-------------|
| **Architecture Type** | {Monolithic / Modular Monolith / Microservices / Serverless} |
| **Communication** | {Synchronous REST / Async Messaging / gRPC / GraphQL} |
| **Deployment Mode** | {Single Machine / Cluster / Containerized / K8s} |

**Architecture Selection Rationale**:

{Explain why this architecture style was chosen, what factors were considered}

---

## 2. Layered Design

### 2.1 Layer Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Presentation Layer                         │
│                                                                 │
│  Responsibility: {Presentation layer responsibility description}    │
│  Components: {Presentation layer component list}                  │
├─────────────────────────────────────────────────────────────────┤
│                    Application Layer                          │
│                                                                 │
│  Responsibility: {Application layer responsibility description}     │
│  Components: {Application layer component list}                   │
├─────────────────────────────────────────────────────────────────┤
│                     Domain Layer                              │
│                                                                 │
│  Responsibility: {Domain layer responsibility description}          │
│  Components: {Domain layer component list}                        │
├─────────────────────────────────────────────────────────────────┤
│                 Infrastructure Layer                          │
│                                                                 │
│  Responsibility: {Infrastructure layer responsibility description}   │
│  Components: {Infrastructure layer component list}                 │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 Layer Dependency Rules

```
Dependency Direction: Upper → Lower (Unidirectional)

✅ Allowed:
   Presentation → Application → Domain → Infrastructure

❌ Forbidden:
   - Lower depends on Upper
   - Cross-layer direct dependency (e.g., Presentation → Infrastructure)
```

### 2.3 Detailed Layer Description

#### Presentation Layer

| Component | Path | Responsibility |
|-----------|------|----------------|
<!-- FOR component in presentation_components -->
| {component.name} | `{component.path}` | {component.responsibility} |
<!-- END FOR -->

#### Application Layer

| Component | Path | Responsibility |
|-----------|------|----------------|
<!-- FOR component in application_components -->
| {component.name} | `{component.path}` | {component.responsibility} |
<!-- END FOR -->

#### Domain Layer

| Component | Path | Responsibility |
|-----------|------|----------------|
<!-- FOR component in domain_components -->
| {component.name} | `{component.path}` | {component.responsibility} |
<!-- END FOR -->

#### Infrastructure Layer

| Component | Path | Responsibility |
|-----------|------|----------------|
<!-- FOR component in infrastructure_components -->
| {component.name} | `{component.path}` | {component.responsibility} |
<!-- END FOR -->

---

## 3. Module Design

### 3.1 Module Dependency Diagram

```mermaid
graph LR
    subgraph Core["Core Modules"]
        {Core module nodes}
    end

    subgraph Business["Business Modules"]
        {Business module nodes}
    end

    subgraph Infrastructure["Infrastructure"]
        {Infrastructure nodes}
    end

    {Module dependency relationships}
```

### 3.2 Module Detailed Description

<!-- FOR module in modules -->
#### {module.name}

| Attribute | Description |
|-----------|-------------|
| **Path** | `{module.path}` |
| **Responsibility** | {module.responsibility} |
| **Public API** | {module.public_api} |
| **Dependencies** | {module.dependencies} |

<!-- END FOR -->

---

## 4. Design Patterns

### 4.1 Design Patterns Used

| Pattern | Application Location | Problem Solved |
|---------|---------------------|---------------|
<!-- FOR pattern in patterns -->
| **{pattern.name}** | `{pattern.location}` | {pattern.problem_solved} |
<!-- END FOR -->

### 4.2 Pattern Implementation Examples

<!-- FOR pattern in pattern_examples -->
#### {pattern.name}

**Problem**: {pattern.problem}

**Solution**: {pattern.solution}

```{pattern.language}
{pattern.code_example}
```

<!-- END FOR -->

---

## 5. Data Flow Design

### 5.1 Request Processing Flow

```mermaid
sequenceDiagram
    participant Client as Client
    participant Gateway as Gateway
    participant Controller as Controller
    participant Service as Service Layer
    participant Repository as Data Layer
    participant DB as Database

    {Request processing sequence steps}
```

### 5.2 Core Business Flows

<!-- FOR flow in business_flows -->
#### {flow.name}

```mermaid
{flow.diagram}
```

**Flow Description**: {flow.description}

<!-- END FOR -->

---

## 6. External Integration

### 6.1 Integration List

| Service | Type | Purpose | Integration Method |
|----------|------|---------|-------------------|
<!-- FOR integration in integrations -->
| {integration.name} | {integration.type} | {integration.purpose} | {integration.method} |
<!-- END FOR -->

### 6.2 Integration Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                     Application Services                    │
├──────────────┬──────────────┬──────────────┬───────────────┤
│              │              │              │               │
▼              ▼              ▼              ▼               ▼
{External service connection diagram}
```

---

## 7. Security Architecture

### 7.1 Authentication Mechanisms

| Mechanism | Implementation Location | Description |
|-----------|----------------------|-------------|
<!-- FOR auth in auth_mechanisms -->
| {auth.name} | `{auth.location}` | {auth.description} |
<!-- END FOR -->

### 7.2 Authorization Model

```
{Authorization model description, e.g., RBAC / ABAC / ACL}

{Permission hierarchy diagram}
```

### 7.3 Security Measures List

| Threat | Protection | Implementation Location |
|--------|-----------|------------------------|
<!-- FOR measure in security_measures -->
| {measure.threat} | {measure.protection} | `{measure.location}` |
<!-- END FOR -->

---

## 8. Scalability Design

### 8.1 Horizontal Scaling Capabilities

| Component | Scaling Method | Notes |
|-----------|---------------|-------|
<!-- FOR scale in horizontal_scaling -->
| {scale.component} | {scale.method} | {scale.notes} |
<!-- END FOR -->

### 8.2 Vertical Splitting Plan

```
Current Architecture → Future Evolution:

{Splitting plan diagram}
```

---

## 9. Technical Decision Records

### 9.1 Key Decisions

<!-- FOR decision in decisions -->
#### ADR-{decision.id}: {decision.title}

| Attribute | Content |
|-----------|---------|
| **Status** | {decision.status} |
| **Context** | {decision.context} |
| **Decision** | {decision.decision} |
| **Rationale** | {decision.rationale} |
| **Consequences** | {decision.consequences} |

<!-- END FOR -->

---

## Appendix

### A. Architecture Evolution History

| Version | Date | Changes |
|---------|------|---------|
<!-- FOR evolution in architecture_history -->
| {evolution.version} | {evolution.date} | {evolution.changes} |
<!-- END FOR -->

### B. References

<!-- FOR ref in references -->
- [{ref.title}]({ref.url})
<!-- END FOR -->
