---
page_title: "fleets_registry_secret Resource - Cyclops"
description: |-
  Creates a private-registry pull Secret in a Fleet pool namespace.
---

# fleets_registry_secret

Creates a `kubernetes.io/dockerconfigjson` Secret named `cua-registry-<name>`, labeled `cua.ai/registry-secret: "true"`, in a pool namespace. Pods and KubeVirt use it to pull the pool's private images (sidecar images included) when `fleets_pool.image_pull_secret` names it.

Fleet never returns Secret contents, so this resource is write-only:

- Changing any argument replaces the Secret (delete, then create).
- Read keeps the state Terraform wrote. A Secret deleted outside Terraform is not detected; taint the resource to recreate it.
- Import is not supported.
- `password` is stored in Terraform state, marked sensitive.

```terraform
resource "fleets_pool" "agent" {
  name                 = "agent-linux"
  cpu_cores            = 4
  memory               = "8Gi"
  container_disk_image = "ghcr.io/example/private-agent@sha256:..."
  image_pull_secret    = "cua-registry-ghcr"
  runtime              = "gvisor"
  replicas             = 1
}

resource "fleets_registry_secret" "ghcr" {
  namespace = fleets_pool.agent.namespace
  name      = "cua-registry-ghcr"
  registry  = "ghcr.io"
  username  = "bot"
  password  = var.ghcr_token
}
```

The Secret lives in the pool's namespace, so it is created right after the pool, and sandboxes that start first retry the pull until it exists. Destroying the pool deletes the namespace and the Secret with it.

## Arguments

- `namespace` - Pool namespace (the `fleets_pool` name). It must already exist.
- `name` - `cua-registry-<dns-label>`.
- `registry` - Registry host as image refs spell it: `ghcr.io`, `registry.example.com:5000`, `docker.io`.
- `username` - Registry user.
- `password` - Password or access token. Sensitive.

## Read-only Attributes

`id` is `<namespace>/<name>`.
