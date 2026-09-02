# Operations and recovery

## Inspect

```bash
fly logs --app YOUR_UNIQUE_APP
fly status --app YOUR_UNIQUE_APP
fly ssh console --app YOUR_UNIQUE_APP --command 'hostpackd doctor'
fly ssh console --app YOUR_UNIQUE_APP --command 'hostpackd backup list'
```

Logs are structured JSON and include pack, phase, generation, and failure context. Runtime state is stored at `/state/runtime/active.json`; do not edit it while Java is running.

## Backup failure

A remote-backup failure leaves the instance directory intact and records `backupPending`. The next clean stop recompresses and retries that generation. Investigate credentials, quota, endpoint, or rclone configuration before deleting any local data.

## Restore after volume loss

Provision a replacement volume of equal or greater size, attach it to the singleton Machine, set the same backup credentials, and run:

```bash
hostpackd backup list
hostpackd backup restore PACK_ID
```

Restore refuses to overwrite an existing instance directory and verifies the archive checksum before the final rename.

## Failed pack

1. Read the provider/launcher error in Fly logs.
2. Confirm Java and memory requirements with `hostpackd doctor` and `packs.yaml`.
3. Confirm the configured exact artifact remains downloadable.
4. Restart the existing Machine and reconnect to the same hostname to retry.

Never point an existing ID at another version. Add and lock a new ID, deploy the image while the Machine is stopped, and let the normal switch path create a separate world.

## Acceptance sequence

Before inviting players, exercise one exact pack from each provider:

1. Status-ping each hostname and verify Java does not start.
2. Join the Modrinth pack, reconnect after installation, create world state, and wait for clean stop.
3. Restore or restart it and confirm the world state persists.
4. Start the CurseForge pack and verify the old pack rejects switching while occupied.
5. Disconnect, wait through the grace period, trigger the switch, and verify both backup generations.
6. Temporarily invalidate backup credentials and confirm shutdown preserves the local world and reports `backupPending`.
7. Force a Java crash and verify Hostpack recovers the same pack before permitting a different one.
