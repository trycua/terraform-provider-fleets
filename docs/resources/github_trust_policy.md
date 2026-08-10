---
page_title: "fleets_github_trust_policy Resource - Fleets"
description: |-
  Grants a GitHub repository access to exact Cua Fleet namespaces through GitHub Actions OIDC.
---

# fleets_github_trust_policy

Creates an owner-scoped GitHub Actions OIDC trust policy. A workflow token is accepted only when its exact GitHub `repository` claim matches and access is limited to the listed Fleet namespaces.

```terraform
resource "fleets_pool" "cua_cli_wif_smoke" {
  name                 = "cua-cli-wif-smoke"
  replicas             = 0
  cpu_cores            = 4
  memory               = "8Gi"
  container_disk_image = "296062593712.dkr.ecr.us-west-2.amazonaws.com/desktop-workspace:latest"
}

resource "fleets_github_trust_policy" "cua_cli_wif_smoke" {
  name       = "cua-cli-wif-smoke"
  repository = "trycua/cua"

  allowed_namespaces = [
    fleets_pool.cua_cli_wif_smoke.name,
  ]

  enabled = true
}
```

Referencing the pool name creates the desired Terraform lifecycle ordering: the pool and its same-named namespace are created before the trust policy, and the policy is removed before the pool is destroyed.

## Arguments

- `name` - Human-readable trust policy name.
- `repository` - Exact GitHub OIDC repository claim in `owner/repo` format.
- `allowed_namespaces` - Non-empty set of exact Fleet namespace DNS labels.
- `enabled` - Whether the policy participates in GitHub OIDC authorization; defaults to `true`.

## Read-only Attributes

- `id` - Trust policy ID.
- `owner_sub` - Owner identity derived from the provider credentials.
- `created_at` - RFC3339 creation timestamp.
- `updated_at` - RFC3339 last-update timestamp.

## Authentication

Terraform must create this resource with a protected user API key or equivalent owner credential. A GitHub WIF token cannot bootstrap its own trust policy because that token is not authorized until the policy exists.

The resulting GitHub workflow requests an OIDC token with issuer `https://token.actions.githubusercontent.com` and audience `fleets`.

## Import

Import an existing owner-scoped trust policy by ID:

```shell
terraform import fleets_github_trust_policy.cua_cli_wif_smoke 12345678-1234-1234-1234-123456789abc
```
