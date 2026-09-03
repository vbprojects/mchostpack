package runtime

import (
	"slices"
	"testing"
)

func TestChildEnvironmentExcludesControlPlaneAndBackupCredentials(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin",
		"HOSTPACK_FLY_API_TOKEN=fly-secret",
		"FLY_API_TOKEN=fly-secret-2",
		"AWS_ACCESS_KEY_ID=backup-key",
		"AWS_SECRET_ACCESS_KEY=backup-secret",
		"AWS_SESSION_TOKEN=session",
		"RCLONE_CONFIG_GDRIVE_TOKEN=drive-secret",
		"CF_API_KEY=needed-by-installer",
		"RCON_PASSWORD=needed-by-server",
	}
	got := childEnvironment(parent)
	for _, forbidden := range []string{
		"HOSTPACK_FLY_API_TOKEN=fly-secret",
		"FLY_API_TOKEN=fly-secret-2",
		"AWS_ACCESS_KEY_ID=backup-key",
		"AWS_SECRET_ACCESS_KEY=backup-secret",
		"AWS_SESSION_TOKEN=session",
		"RCLONE_CONFIG_GDRIVE_TOKEN=drive-secret",
	} {
		if slices.Contains(got, forbidden) {
			t.Fatalf("sensitive variable reached child: %s", forbidden)
		}
	}
	for _, required := range []string{"PATH=/usr/bin", "CF_API_KEY=needed-by-installer", "RCON_PASSWORD=needed-by-server"} {
		if !slices.Contains(got, required) {
			t.Fatalf("required variable missing from child: %s", required)
		}
	}
}

func TestChildIDRejectsInvalidValue(t *testing.T) {
	t.Setenv("HOSTPACK_MINECRAFT_UID", "not-a-number")
	if _, err := childID("HOSTPACK_MINECRAFT_UID", 1000); err == nil {
		t.Fatal("accepted invalid child UID")
	}
}
