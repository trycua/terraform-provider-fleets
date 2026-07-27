---
page_title: "fleets_claim Resource - Fleets"
description: |-
  Claims one sandbox from a Cua Fleet pool.
---

# fleets_claim

Creates one `OSGymSandboxClaim` in a pool's namespace and waits until Fleet
reports it as `Bound`. Destroy deletes the claim, which releases its sandbox
back to the warm pool according to the claim controller's lifecycle policy.

```terraform
resource "fleets_pool" "fixed_gvisor" {
  name                 = "fixed-gvisor"
  replicas             = 1
  runtime              = "gvisor"
  cpu_cores            = 2
  memory               = "4Gi"
  container_disk_image = "example.invalid/cua/gvisor:latest"

  service {
    name        = "mcp"
    target_port = 3001
    protocol    = "TCP"
  }
}

resource "fleets_claim" "mcp" {
  # This reference ensures Terraform destroys the claim before the pool.
  pool_name  = fleets_pool.fixed_gvisor.name
  claim_name = "fixed-gvisor-mcp"
}
```

A fixed pool must set `replicas = 1`, `runtime = "gvisor"`, and omit the
`autoscaling` block.

## Arguments

- `pool_name` - Name of the pool to claim from. This is also the claim namespace.
- `claim_name` - Optional stable DNS-label claim name. The provider generates one when omitted.
- `create_timeout` - Positive Go duration for creation and the wait for `Bound`; defaults to `10m`.
- `delete_timeout` - Positive Go duration for release and deletion observation; defaults to `2m`.

Changing `pool_name` or `claim_name` replaces the claim.

## Read-only Attributes

- `id` - Stable `pool_name/claim_name` identifier.
- `namespace` - Namespace containing the claim.
- `phase` - Lifecycle phase reported by the claim: `Pending`, `Bound`, or `Failed`.
- `sandbox_name` - Bound sandbox name when available.
- `sandbox_service` - Authoritative in-cluster DNS reported by the claim controller.

## Gateway Endpoint Contract

The current claim CRD status exposes one unstructured in-cluster service name
at `status.sandbox.service`. The current Cyclops service gateway accepts a
relative authenticated proxy route, `/api/svc/{namespace}/{service}/{path}`;
it does not return a public service URL or an endpoint map keyed by pool
service name. Therefore this provider intentionally does not compute an MCP
URL or publish a fabricated `services` map.

For `trycua/cloud` to output a claimed MCP URL safely, the upstream claim or
gateway API must return an authoritative externally addressable endpoint,
including the service name/path and any required non-secret authentication
contract. If the API returns credentials, those values should be separate
sensitive Terraform attributes rather than embedded in a URL.

## Import

The API supports reliable GET and DELETE only by namespace and claim name, so
import uses the stable composite identifier:

```shell
terraform import fleets_claim.mcp fixed-gvisor/fixed-gvisor-mcp
```
