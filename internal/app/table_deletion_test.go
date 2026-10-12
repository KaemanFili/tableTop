package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTableDeletionOwnershipPersistenceAndQuota(t *testing.T) {
	dir := t.TempDir()
	limits, err := NewStorageLimits(dir, 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	library, err := NewTableLibrary(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	table, err := library.Create("First", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Join(table.ID, "player"); err != nil {
		t.Fatal(err)
	}
	if err := library.BeginDelete(table.ID, "player"); !errors.Is(err, ErrTableNotOwner) {
		t.Fatal("player could begin deletion")
	}
	if err := library.FinishDelete(table.ID, "owner"); err == nil {
		t.Fatal("deletion finished without persisted intent")
	}
	limits.total = storedBytes(t, dir)
	if err := library.BeginDelete(table.ID, "owner"); err != nil {
		t.Fatal("full quota prevented deletion:", err)
	}
	library, err = NewTableLibrary(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(library.ForUser("owner")) != 1 || len(library.ForUser("player")) != 0 {
		t.Fatal("pending deletion was not restricted to owner")
	}
	if _, err := library.Join(table.ID, "owner"); err == nil {
		t.Fatal("pending tabletop could be joined")
	}
	if err := library.FinishDelete(table.ID, "player"); !errors.Is(err, ErrTableNotOwner) {
		t.Fatal("player could finish deletion")
	}
	if err := library.FinishDelete(table.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	library, err = NewTableLibrary(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := library.ByID(table.ID); exists {
		t.Fatal("deleted tabletop returned after reload")
	}
}

func TestTableFileCleanupRejectsUnsafeIDs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "../..", "../../keep.txt", "dev"} {
		if err := RemoveTableFiles(dir, id, nil); err == nil {
			t.Fatalf("accepted unsafe ID %q", id)
		}
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("cleanup escaped session directory")
	}
}
