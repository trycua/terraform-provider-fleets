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
  # This reference makes Terraform release the claim before deleting the pool.
  pool_name  = fleets_pool.fixed_gvisor.name
  claim_name = "fixed-gvisor-mcp"
}

output "claimed_sandbox" {
  value = fleets_claim.mcp.sandbox_name
}

# The Fleet claim API currently returns only sandbox_service, its in-cluster
# DNS name. It does not return an externally addressable MCP URL.
