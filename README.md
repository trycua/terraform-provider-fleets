# Terraform Provider for Cua Fleets

This provider manages `OSGymWorkspacePool` resources through the Fleet API. It also manages single-sandbox `OSGymSandboxClaim` resources through the Kubernetes proxy API. It uses the same user-key OAuth credentials and tenant-scoped authorization as the Fleet Python SDK and dashboard.

## Development

```bash
go test ./...
go build ./...

# Real Terraform protocol + kube-apiserver/CRD acceptance test
KUBEBUILDER_ASSETS="$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.19 use 1.31.0 -p path)" \
  FLEETS_CRD_DIR=/path/to/clusters/base/osgym \
  TF_ACC=1 go test -tags=acceptance ./internal/provider -run TestAccPoolLifecycle -v
```

The provider source address is `trycua/fleets`. A pool owns the same-named Fleet namespace: create bootstraps the namespace, and destroy deletes the pool followed by that namespace. A claim references `pool_name`; Terraform uses that reference to release the claim before deleting its pool.

## Authentication

Create a user API key in Cua and configure its returned `client_id`, `client_secret`, and `token_url`. For short-lived workflows, `access_token` can be supplied instead.

All provider arguments support environment variables:

- `CYCLOPS_ENDPOINT`
- `CYCLOPS_ACCESS_TOKEN`
- `CYCLOPS_CLIENT_ID`
- `CYCLOPS_CLIENT_SECRET`
- `CYCLOPS_TOKEN_URL`

## Pool resource generation

The Terraform models, schema, CRD-derived descriptions, enum validators, numeric validators, and nested object types are generated deterministically from:

- `clusters/base/osgym/crd.yaml`, the production `OSGymWorkspacePool` CRD
- `internal/provider/generate/pool_mapping.json`, the explicit CRD-to-Terraform shape mapping

From `cyclops-cs/terraform-provider-fleets/`, run:

```bash
go generate ./...
```

`internal/provider/pool_generated.go` is committed. CI regenerates it and fails on drift. CRUD, namespace ownership, import behavior, default normalization, and API error handling remain handwritten in `internal/provider/pool_resource.go`.

## Single-sandbox claims

`fleets_claim` creates one `OSGymSandboxClaim` for an existing pool and waits
for the claim controller to report `Bound`. It exposes the claim, namespace,
phase, sandbox name, and the claim controller's authoritative in-cluster
service name. See `docs/resources/claim.md` and
`examples/resources/fleets_claim/resource.tf`.

The current Fleet API does not return an externally addressable MCP URL or a
named endpoint map. The provider deliberately does not synthesize either; the
upstream claim/gateway API needs to publish that contract before a consumer can
safely output a claimed MCP service URL.
