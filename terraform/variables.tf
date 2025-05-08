variable "location" {
  type        = string
  default     = "brazilsouth"
  description = "value of location"
}

variable "kaito_gpu_provisioner_version" {
  type        = string
  default     = "0.3.3"
  description = "kaito gpu provisioner version"
}

variable "kaito_workspace_version" {
  type        = string
  default     = "0.4.5"
  description = "kaito workspace version"
}

variable "registry_repository_name" {
  type        = string
  default     = "fine-tuned-adapters/kubernetes"
  description = "container registry repository name"
}
