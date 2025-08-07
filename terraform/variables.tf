variable "location" {
  type        = string
  default     = "brazilsouth"
  description = "value of location"
}

variable "kaito_gpu_provisioner_version" {
  type        = string
  default     = "0.3.5"
  description = "kaito gpu provisioner version"
}

variable "kaito_workspace_version" {
  type        = string
  default     = "0.6.0"
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
  default     = "0.6.0"
  description = "KAITO RAGEngine version"
}
