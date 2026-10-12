package app

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func storedBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var size int64
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return size
}

func TestStorageLimitsRollbackImagesWhenCatalogExceedsLimit(t *testing.T) {
	for _, kind := range []string{"unit", "map"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			limits, err := NewStorageLimits(root, 4096, 1024)
			if err != nil {
				t.Fatal(err)
			}
			// The image fits; committing its metadata would cross the cap.
			image := bytes.Repeat([]byte("x"), 1023)
			if kind == "unit" {
				library, err := NewUnitLibrary(root, limits)
				if err != nil {
					t.Fatal(err)
				}
				_, err = library.Create("Scout", "npc", SizeMedium, DefaultImageFit(), image)
				if !errors.Is(err, ErrStorageLimit) || len(library.Units()) != 2 {
					t.Fatalf("unit creation at quota: %v", err)
				}
			} else {
				library, err := NewMapLibrary(root, limits)
				if err != nil {
					t.Fatal(err)
				}
				_, err = library.Create(MapDefinition{Name: "Forest", Width: 20, Height: 20}, image)
				if !errors.Is(err, ErrStorageLimit) || len(library.Maps()) != 0 {
					t.Fatalf("map creation at quota: %v", err)
				}
			}
			if used := storedBytes(t, root); used != 0 {
				t.Fatalf("failed creation retained %d bytes", used)
			}
		})
	}
}

func TestStorageLimitsSaveAndReclaimAtCapacity(t *testing.T) {
	root := t.TempDir()
	limits, _ := NewStorageLimits(root, 4096, 1024)
	library, err := NewMapLibrary(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := library.Create(MapDefinition{Name: "Forest", Width: 20, Height: 20}, []byte("artwork"))
	if err != nil {
		t.Fatal(err)
	}
	// Treat the current disk usage as the cap, as if the host lowered it.
	limits.table = storedBytes(t, root)
	before, _ := os.ReadFile(filepath.Join(root, "maps.json"))
	err = library.SaveBoard(definition, SavedBoard{Units: []UnitData{{ID: "U1", Name: "Scout"}}})
	if !errors.Is(err, ErrStorageLimit) {
		t.Fatalf("growing save at capacity: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "maps.json"))
	saved, _ := library.MapByID(definition.ID)
	if !bytes.Equal(before, after) || saved.Board != nil {
		t.Fatal("rejected save replaced the previous map")
	}
	// Existing saved data stays readable after restart, even above a new cap.
	limits.table--
	reloaded, err := NewMapLibrary(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Delete(definition.ID); err != nil {
		t.Fatalf("deletion blocked by quota: %v", err)
	}
	if _, err := reloaded.Create(MapDefinition{Name: "Forest", Width: 20, Height: 20}, []byte("artwor")); err != nil {
		t.Fatalf("could not reuse reclaimed capacity: %v", err)
	}
}

func TestStorageLimitsSharedAcrossConcurrentTables(t *testing.T) {
	root := t.TempDir()
	limits, _ := NewStorageLimits(root, 4096, 1024)
	var workers sync.WaitGroup
	start := make(chan struct{})
	var successMu sync.Mutex
	successes := 0
	for index := range 16 {
		library, err := NewMapLibrary(filepath.Join(root, "sessions", strconv.Itoa(index)), limits)
		if err != nil {
			t.Fatal(err)
		}
		workers.Go(func() {
			<-start
			_, err := library.Create(MapDefinition{Name: "Forest", Width: 20, Height: 20}, bytes.Repeat([]byte("x"), 512))
			if err == nil {
				successMu.Lock()
				successes++
				successMu.Unlock()
			} else if !errors.Is(err, ErrStorageLimit) {
				t.Error(err)
			}
		})
	}
	close(start)
	workers.Wait()
	if successes == 0 || successes == 16 {
		t.Fatalf("unexpected accepted upload count: %d", successes)
	}
	if used := storedBytes(t, root); used > limits.total {
		t.Fatalf("concurrent uploads exceeded quota: %d", used)
	}
	files, _ := filepath.Glob(filepath.Join(root, "sessions", "*", "maps", "*.png"))
	if len(files) != successes {
		t.Fatal("rejected concurrent uploads left orphaned artwork")
	}
}

func TestStorageLimitsTableIsolationAndMembership(t *testing.T) {
	root := t.TempDir()
	limits, _ := NewStorageLimits(root, 10000, 1024)
	first, _ := NewMapLibrary(filepath.Join(root, "sessions", "first"), limits)
	second, _ := NewMapLibrary(filepath.Join(root, "sessions", "second"), limits)
	definition := MapDefinition{Name: "Forest", Width: 20, Height: 20}
	if _, err := first.Create(definition, bytes.Repeat([]byte("x"), 800)); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Create(definition, bytes.Repeat([]byte("x"), 200)); !errors.Is(err, ErrStorageLimit) {
		t.Fatalf("table cap was bypassed: %v", err)
	}
	if _, err := second.Create(definition, bytes.Repeat([]byte("x"), 800)); err != nil {
		t.Fatalf("another table inherited the first table's usage: %v", err)
	}
	catalog, _ := NewTableLibrary(root, limits)
	table, err := catalog.Create("First", "owner")
	if err != nil {
		t.Fatal(err)
	}
	limits.total = storedBytes(t, root)
	if _, err := catalog.Create("Second", "owner"); !errors.Is(err, ErrStorageLimit) {
		t.Fatalf("session metadata bypassed global cap: %v", err)
	}
	if _, err := catalog.Join(table.ID, "player"); !errors.Is(err, ErrStorageLimit) {
		t.Fatalf("membership bypassed global cap: %v", err)
	}
	saved, _ := catalog.ByID(table.ID)
	if saved.HasMember("player") {
		t.Fatal("rejected membership was committed")
	}
}

func TestStorageLimitEnvironment(t *testing.T) {
	t.Setenv("TABLETOP_STORAGE_LIMIT_BYTES", "")
	t.Setenv("TABLETOP_SESSION_STORAGE_LIMIT_BYTES", "")
	limits, err := StorageLimitsFromEnvironment(t.TempDir())
	if err != nil || limits.total != 5<<30 || limits.table != 512<<20 {
		t.Fatalf("default limits: %+v %v", limits, err)
	}
	for _, invalid := range []string{"0", "-1", "bad", "9223372036854775808"} {
		t.Setenv("TABLETOP_STORAGE_LIMIT_BYTES", invalid)
		if _, err := StorageLimitsFromEnvironment(t.TempDir()); err == nil {
			t.Fatalf("accepted invalid cap: %q", invalid)
		}
	}
	t.Setenv("TABLETOP_STORAGE_LIMIT_BYTES", "2048")
	t.Setenv("TABLETOP_SESSION_STORAGE_LIMIT_BYTES", "1024")
	limits, err = StorageLimitsFromEnvironment(t.TempDir())
	if err != nil || limits.total != 2048 || limits.table != 1024 {
		t.Fatalf("configured limits: %+v %v", limits, err)
	}
	t.Setenv("TABLETOP_SESSION_STORAGE_LIMIT_BYTES", "4096")
	if _, err := StorageLimitsFromEnvironment(t.TempDir()); err == nil {
		t.Fatal("accepted a tabletop cap larger than the global cap")
	}
}
