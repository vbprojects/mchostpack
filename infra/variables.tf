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
  description = "Bootstrap shared CPU count for a new or replaced Machine. Hostpack resizes it per pack."
  type        = number
  default     = 2
}

variable "memory_mb" {
  description = "Bootstrap guest memory for a new or replaced Machine. Hostpack resizes it per pack."
  type        = number
  default     = 2048
}

variable "volume_size_gb" {
  type    = number
  default = 15
}

variable "snapshot_retention" {
  type    = number
  default = 7
}
