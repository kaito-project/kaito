locals {
  random_name = "kaitodemo${random_integer.example.result}"

  # kube_config carries no credentials on a Microsoft Entra ID cluster, so use the local admin account there.
  kube_config = var.enable_aad ? one(azurerm_kubernetes_cluster.example.kube_admin_config) : one(azurerm_kubernetes_cluster.example.kube_config)
}
