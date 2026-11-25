terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "=4.80.0"
    }

    random = {
      source  = "hashicorp/random"
      version = "=3.9.0"
    }

    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "=3.2.1"
    }

    helm = {
      source  = "hashicorp/helm"
      version = "=3.2.0"
    }
  }
}

provider "azurerm" {
  features {
    resource_group {
      prevent_deletion_if_contains_resources = false
    }
  }
}

provider "kubernetes" {
  host                   = local.kube_config.host
  username               = local.kube_config.username
  password               = local.kube_config.password
  client_certificate     = base64decode(local.kube_config.client_certificate)
  client_key             = base64decode(local.kube_config.client_key)
  cluster_ca_certificate = base64decode(local.kube_config.cluster_ca_certificate)
}

provider "helm" {
  kubernetes = {
    host                   = local.kube_config.host
    username               = local.kube_config.username
    password               = local.kube_config.password
    client_certificate     = base64decode(local.kube_config.client_certificate)
    client_key             = base64decode(local.kube_config.client_key)
    cluster_ca_certificate = base64decode(local.kube_config.cluster_ca_certificate)
  }
}

data "azurerm_client_config" "current" {}

resource "random_integer" "example" {
  min = 1000
  max = 9999
}

resource "azurerm_resource_group" "example" {
  name     = "rg-${local.random_name}"
  location = var.location
}
