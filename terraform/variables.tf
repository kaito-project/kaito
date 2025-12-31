variable "location" {
  type        = string
  default     = "brazilsouth"
  description = "value of location"
}

variable "node_pool_count" {
  type        = number
  default     = 1
  description = "Number of nodes in the AKS node pool"
}

variable "node_pool_vm_size" {
  type        = string
  default     = "standard_d2s_v6"
  description = "VM size for the AKS node pool"
}

variable "kaito_gpu_provisioner_version" {
  type        = string
  default     = "0.3.8"
  description = "kaito gpu provisioner version"
}

variable "kaito_workspace_version" {
  type        = string
  default     = "0.8.0"
  description = "kaito workspace version"
}

variable "registry_repository_name" {
  type        = string
  default     = "fine-tuned-adapters/kubernetes"
  description = "container registry repository name"
}

variable "deploy_kaito_ragengine" {
  type        = bool
  default     = true
  description = "whether to deploy the KAITO RAGEngine"
}

variable "kaito_ragengine_version" {
  type        = string
  default     = "0.7.0"
  description = "KAITO RAGEngine version"
}

variable "kaito_workspace_features" {
  type        = list(string)
  default     = ["gatewayAPIInferenceExtension"]
  description = "List of KAITO workspace features to enable"
}
