resource "fleets_registry_secret" "ghcr" {
  namespace = fleets_pool.example.namespace
  name      = "cua-registry-ghcr"
  registry  = "ghcr.io"
  username  = "bot"
  password  = var.ghcr_token
}
