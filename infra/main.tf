provider "fly" {
  org_slug = var.org_slug
}

resource "fly_app" "hostpack" {
  name     = var.app_name
  org_slug = var.org_slug
}

resource "fly_ip_address" "minecraft" {
  app  = fly_app.hostpack.name
  type = "v4"
}

resource "fly_volume" "state" {
  app                 = fly_app.hostpack.name
  name                = "hostpack_state"
  region              = var.region
  size_gb             = var.volume_size_gb
  encrypted           = true
  auto_backup_enabled = true
  snapshot_retention  = var.snapshot_retention
}

resource "fly_machine" "hostpack" {
  app            = fly_app.hostpack.name
  name           = "hostpack-singleton"
  region         = var.region
  image          = var.image_ref
  skip_launch    = true
  desired_status = "stopped"
  auto_destroy   = false

  env = {
    HOSTPACK_CONFIG  = "/app/config/packs.yaml"
    HOSTPACK_LOCK    = "/app/config/packs.lock.json"
    HOSTPACK_STATE   = "/state"
    HOSTPACK_FLY_APP = fly_app.hostpack.name
  }

  guest {
    cpu_kind  = "shared"
    cpus      = var.cpus
    memory_mb = var.memory_mb
  }

  lifecycle {
    # hostpackd changes these fields before launching each pack. OpenTofu owns
    # the bootstrap size used by replacement Machines, not the live size.
    ignore_changes = [guest]
  }

  mount {
    volume = fly_volume.state.id
    path   = "/state"
  }

  restart {
    policy      = "on-failure"
    max_retries = 3
  }

  service {
    protocol             = "tcp"
    internal_port        = 25565
    autostart            = true
    autostop             = false
    min_machines_running = 0

    concurrency {
      type       = "connections"
      soft_limit = 96
      hard_limit = 128
    }

    port {
      port = 25565
    }
  }

  service {
    protocol             = "tcp"
    internal_port        = 8080
    autostart            = true
    autostop             = false
    min_machines_running = 0

    concurrency {
      type       = "connections"
      soft_limit = 20
      hard_limit = 40
    }

    port {
      port        = 80
      handlers    = ["http"]
      force_https = true
    }

    port {
      port     = 443
      handlers = ["tls", "http"]
    }
  }
}
