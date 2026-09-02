terraform {
  required_version = ">= 1.11.0, < 2.0.0"

  required_providers {
    fly = {
      source  = "stategraph/fly"
      version = "= 0.2.4"
    }
  }

  backend "s3" {
    use_lockfile = true
    encrypt      = true
  }
}
