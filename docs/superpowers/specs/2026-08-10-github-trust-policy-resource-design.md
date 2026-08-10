# GitHub Trust Policy Resource Design

## Goal

Add a first-class `fleets_github_trust_policy` Terraform resource that manages
the Fleets GitHub Actions OIDC trust-policy API.

## Configuration

```hcl
resource "fleets_github_trust_policy" "cua_cli_wif_smoke" {
  name       = "cua-cli-wif-smoke"
  repository = "trycua/cua"

  allowed_namespaces = [
    fleets_pool.cua_cli_wif_smoke.name,
  ]

  enabled = true
}
```

`allowed_namespaces` is a Terraform set. The provider sends a stable sorted
list to the API so ordering differences do not produce drift. The resource
validates repository names as `owner/repo` and namespaces as DNS-1123 labels
with the same 63-character limit enforced by Fleets.

## State

The resource stores these configurable attributes:

- `name`
- `repository`
- `allowed_namespaces`
- `enabled`

It also stores the API-provided computed attributes `id`, `owner_sub`,
`created_at`, and `updated_at`. `owner_sub` is always derived from the provider
credentials and cannot be configured.

## Lifecycle

- Create calls `POST /api/github-trust-policies`.
- Read calls `GET /api/github-trust-policies` and selects the state ID from the
  owner-scoped response.
- Update calls `PATCH /api/github-trust-policies/{id}` with the complete mutable
  policy shape.
- Delete calls `DELETE /api/github-trust-policies/{id}`.
- Import accepts the policy ID.

If Read cannot find the imported or previously managed ID, the resource is
removed from Terraform state. Delete treats an already absent policy as
success.

## Authentication

Trust-policy requests reuse the provider's existing authentication inputs:

- `access_token`, or `CYCLOPS_ACCESS_TOKEN`
- `client_id`, `client_secret`, and `token_url`, or their environment variables

The provider keeps the native Fleet SDK client for pool operations and adds an
authenticated JSON API client for trust-policy routes. OAuth client credentials
are exchanged using `grant_type=client_credentials`; tokens are cached until
shortly before expiry. Bearer tokens remain sensitive and are never written to
Terraform state or diagnostics.

The resource cannot bootstrap itself using a GitHub WIF token because the
authorizing policy does not exist yet. Terraform apply must use a protected
user API key or equivalent owner credential.

## Provider Integration

Provider configuration returns one internal client object containing both the
native Fleet SDK client and the authenticated JSON API client. Existing pool
resources use the embedded native client without changing their public schema
or lifecycle behavior.

## Documentation And Tests

Provider registration tests cover the new resource type. Unit and protocol
tests cover schema semantics, static-token and OAuth authentication, CRUD,
import, missing-resource handling, API errors, and namespace ordering. The
resource documentation and an example configuration ship with the provider.
