variable "app_name" {
  description = "Globally unique Fly application name."
  type        = string
}

variable "org_slug" {
  description = "Fly organization slug."
  type        = string
  default     = "personal"
}

variable "region" {
  description = "Fly primary region."
  type        = string
  default     = "iad"
}

variable "image_ref" {
  description = "Immutable container image reference including @sha256 digest."
  type        = string
  validation {
    condition     = can(regex("@sha256:[0-9a-f]{64}$", var.image_ref))
    error_message = "image_ref must be digest-pinned."
  }
}

variable "cpus" {
  type    = number
  default = 4
}

variable "memory_mb" {
  type    = number
  default = 12288
}

variable "volume_size_gb" {
  type    = number
  default = 30
}

variable "snapshot_retention" {
  type    = number
  default = 7
}
