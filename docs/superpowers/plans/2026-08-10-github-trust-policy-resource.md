# GitHub Trust Policy Resource Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a production-ready `fleets_github_trust_policy` Terraform resource with owner-scoped CRUD, import, validation, authentication, tests, and documentation.

**Architecture:** Provider configuration returns a shared internal client that embeds the existing native Fleet SDK client and adds authenticated JSON requests for control-plane APIs. The trust-policy resource maps Terraform state to the existing `/api/github-trust-policies` endpoints, while the pool resource continues using the embedded SDK unchanged.

**Tech Stack:** Go 1.25.8, Terraform Plugin Framework, native Fleets UniFFI SDK, `net/http`, Go unit tests and Terraform protocol tests.

## Global Constraints

- Resource type is exactly `fleets_github_trust_policy`.
- `owner_sub` is computed and never configurable.
- `allowed_namespaces` is a non-empty set of DNS-1123 labels, each at most 63 characters.
- `repository` uses exact `owner/repo` syntax.
- Static and OAuth credentials reuse the existing provider configuration.
- Bearer tokens never enter Terraform state or diagnostics.
- Missing policies are removed from state; deleting an absent policy succeeds.

---

### Task 1: Shared Provider Client

**Files:**
- Create: `internal/provider/client.go`
- Create: `internal/provider/client_test.go`
- Modify: `internal/provider/provider.go`
- Modify: `internal/provider/pool_resource.go`
- Modify: `internal/provider/sdk_client_test.go`

**Interfaces:**
- Produces: `configuredClient`, `newConfiguredClient(...)`, and authenticated JSON request helpers.
- Preserves: promoted native SDK methods used by `poolResource`.

- [ ] **Step 1: Write failing tests for static-token JSON requests and OAuth token exchange/cache.**
- [ ] **Step 2: Run focused client tests and confirm the new interfaces are missing.**
- [ ] **Step 3: Implement `configuredClient`, OAuth token acquisition, expiry caching, and JSON error handling.**
- [ ] **Step 4: Configure pool resources with `*configuredClient` and preserve native SDK behavior.**
- [ ] **Step 5: Run client and pool SDK tests until they pass.**

### Task 2: Trust Policy Resource Schema And Mapping

**Files:**
- Create: `internal/provider/github_trust_policy_resource.go`
- Create: `internal/provider/github_trust_policy_resource_test.go`
- Modify: `internal/provider/provider.go`
- Modify: `internal/provider/provider_test.go`

**Interfaces:**
- Produces: `NewGitHubTrustPolicyResource()` and `githubTrustPolicyResourceModel`.
- Consumes: authenticated JSON helpers from Task 1.

- [ ] **Step 1: Write failing metadata, registration, schema, validation, and state-mapping tests.**
- [ ] **Step 2: Run focused resource tests and confirm the resource is absent.**
- [ ] **Step 3: Implement resource metadata, schema, validators, model conversion, and provider registration.**
- [ ] **Step 4: Run focused schema and mapping tests until they pass.**

### Task 3: CRUD And Import

**Files:**
- Modify: `internal/provider/github_trust_policy_resource.go`
- Modify: `internal/provider/github_trust_policy_resource_test.go`

**Interfaces:**
- Implements: Create, Read, Update, Delete, Configure, and ImportState.
- Uses: `POST`, owner-scoped list `GET`, `PATCH`, and `DELETE` trust-policy routes.

- [ ] **Step 1: Write failing HTTP-backed tests for create/read/update/delete and sorted namespaces.**
- [ ] **Step 2: Write failing tests for import, missing read, missing delete, and API errors.**
- [ ] **Step 3: Implement minimal lifecycle methods and diagnostics.**
- [ ] **Step 4: Run all trust-policy resource tests until they pass.**

### Task 4: Documentation And Example

**Files:**
- Create: `docs/resources/github_trust_policy.md`
- Create: `examples/resources/fleets_github_trust_policy/resource.tf`
- Modify: `docs/index.md`
- Modify: `README.md`

**Interfaces:**
- Documents: full schema, import command, bootstrap authentication requirement, and pool reference example.

- [ ] **Step 1: Add the resource example using `fleets_pool.example.name`.**
- [ ] **Step 2: Document arguments, computed attributes, lifecycle, import, and WIF bootstrap limitation.**
- [ ] **Step 3: Link the resource from provider documentation and README.**
- [ ] **Step 4: Scan documentation for stale manual-only trust-policy guidance.**

### Task 5: Verification And Pull Request

**Files:**
- Modify as required by formatting or test findings only.

**Interfaces:**
- Verifies: focused unit tests, full Go test suite, formatting, build, and clean Git diff.

- [ ] **Step 1: Run `gofmt` on changed Go files.**
- [ ] **Step 2: Run focused provider tests.**
- [ ] **Step 3: Run `go test ./...` and `go build ./...` with the native SDK bindings configured.**
- [ ] **Step 4: Review the final diff for secrets, generated drift, and unrelated changes.**
- [ ] **Step 5: Commit, push `codex/github-trust-policy-resource`, and open a ready pull request.**
