resource "azurerm_container_registry" "example" {
  resource_group_name    = azurerm_resource_group.example.name
  location               = azurerm_resource_group.example.location
  name                   = "acr${local.random_name}"
  sku                    = "Standard"
  admin_enabled          = false
  anonymous_pull_enabled = false
}

resource "azurerm_container_registry_scope_map" "example" {
  name                    = "default"
  container_registry_name = azurerm_container_registry.example.name
  resource_group_name     = azurerm_resource_group.example.name

  actions = [
    "repositories/${var.registry_repository_name}/content/read",
    "repositories/${var.registry_repository_name}/content/write"
  ]
}

resource "azurerm_container_registry_token" "example" {
  name                    = "default"
  container_registry_name = azurerm_container_registry.example.name
  resource_group_name     = azurerm_resource_group.example.name
  scope_map_id            = azurerm_container_registry_scope_map.example.id
}

resource "azurerm_container_registry_token_password" "example" {
  container_registry_token_id = azurerm_container_registry_token.example.id

  password1 {
    expiry = timeadd(timestamp(), "168h") # 7 days
  }
}
