output "app_name" {
  value = fly_app.hostpack.name
}

output "dedicated_ipv4" {
  description = "Create a wildcard A record pointing at this address."
  value       = fly_ip_address.minecraft.address
}

output "machine_id" {
  value = fly_machine.hostpack.id
}

output "volume_id" {
  value = fly_volume.state.id
}
