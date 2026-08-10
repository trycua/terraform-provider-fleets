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
