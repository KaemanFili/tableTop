package main

import "testing"

func TestDevelopmentRequiresExplicitFlag(t *testing.T) {
	for _, name := range []string{"OAUTH2_CLIENT_ID", "OAUTH2_CLIENT_SECRET", "OAUTH2_CLIENT_SECRET_FILE", "OAUTH2_REDIRECT_URL", "TABLETOP_ADMIN_USER_IDS"} {
		t.Setenv(name, "")
	}
	root := t.TempDir()
	if _, err := configureAuthentication(false, root, nil); err == nil {
		t.Fatal("normal startup allowed missing server access settings")
	}
	t.Setenv("TABLETOP_ADMIN_USER_IDS", "123")
	if _, err := configureAuthentication(false, root, nil); err == nil {
		t.Fatal("administrator IDs allowed missing OAuth settings")
	}
	// Explicit development ignores OAuth secrets, even an unreadable path.
	t.Setenv("OAUTH2_CLIENT_SECRET_FILE", "/missing/oauth2-secret")
	if manager, err := configureAuthentication(true, root, nil); err != nil || manager != nil {
		t.Fatalf("explicit development startup: %v", err)
	}
}
