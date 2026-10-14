package persistence

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInvitationsAdmissionReplayExpiryAndPersistence(t *testing.T) {
	root := t.TempDir()
	access, err := NewAccessLibrary(root, []string{"admin"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAccessLibrary(t.TempDir(), nil, nil); err == nil {
		t.Fatal("server bootstrapped without administrators")
	}
	if err := access.Admit("stranger", ""); !errors.Is(err, ErrInvitation) {
		t.Fatal("uninvited player admitted")
	}
	if err := access.Admit("admin", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := access.CreateInvitation("stranger"); err == nil {
		t.Fatal("non-admin issued an invitation")
	}
	code, err := access.CreateInvitation("admin")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "access.json"))
	if strings.Contains(string(data), code) {
		t.Fatal("usable invitation persisted in plaintext")
	}
	if err := access.Admit("player", code); err != nil {
		t.Fatal(err)
	}
	if err := access.Admit("second-player", code); !errors.Is(err, ErrInvitation) {
		t.Fatal("single-use invitation replayed")
	}
	reloaded, err := NewAccessLibrary(root, []string{"admin"}, nil)
	if err != nil || !reloaded.Allowed("player") || !reloaded.IsAdmin("admin") {
		t.Fatalf("admission did not persist: %v", err)
	}
	if err := reloaded.Admit("player", ""); err != nil {
		t.Fatal("returning member needed another invite")
	}
	code, _ = reloaded.CreateInvitation("admin")
	reloaded.data.Invites[0].Expires = time.Now().Add(-time.Second)
	if err := reloaded.Admit("new-player", code); !errors.Is(err, ErrInvitation) {
		t.Fatal("expired invitation accepted")
	}
	code, _ = reloaded.CreateInvitation("admin")
	if err := reloaded.RevokeInvitation("admin", invitationHash(code)); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Admit("new-player", code); !errors.Is(err, ErrInvitation) {
		t.Fatal("revoked invitation accepted")
	}
}

func TestConcurrentInvitationOnlyAdmitsOnePlayer(t *testing.T) {
	access, _ := NewAccessLibrary(t.TempDir(), []string{"admin"}, nil)
	code, _ := access.CreateInvitation("admin")
	var workers sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"first", "second"} {
		workers.Go(func() { results <- access.Admit(id, code) })
	}
	workers.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, ErrInvitation) {
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatalf("one invitation admitted %d players", accepted)
	}
}

func TestFailedAdmissionKeepsInvitationUsable(t *testing.T) {
	root := t.TempDir()
	limits, _ := NewStorageLimits(root, 4096, 4096)
	access, _ := NewAccessLibrary(root, []string{"admin"}, limits)
	code, _ := access.CreateInvitation("admin")
	// A long ID requires more metadata than removing the hashed invitation frees.
	limits.total = storedBytes(t, root)
	id := strings.Repeat("p", 512)
	if err := access.Admit(id, code); !errors.Is(err, ErrStorageLimit) {
		t.Fatalf("expected storage rejection: %v", err)
	}
	if access.Allowed(id) || len(access.Invitations("admin")) != 1 {
		t.Fatal("failed persistence consumed invitation or admitted user")
	}
	limits.total = 4096
	if err := access.Admit(id, code); err != nil {
		t.Fatal(err)
	}
}
