package plugins

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"tableTop/main/internal/app"
)

func mongoFixture(t *testing.T) (*app.MongoStore, *mongo.Database, string) {
	t.Helper()
	uri := os.Getenv("TABLETOP_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("TABLETOP_TEST_MONGODB_URI not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	name := "tabletop_plugin_test_" + rand.Text()
	store, err := app.NewMongoStore(ctx, uri, name)
	if err != nil {
		t.Fatal(err)
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetTimeout(10 * time.Second))
	if err != nil {
		store.Close(ctx)
		t.Fatal(err)
	}
	db := client.Database(name)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := db.Drop(ctx); err != nil {
			t.Error(err)
		}
		store.Close(ctx)
		client.Disconnect(ctx)
	})
	return store, db, rand.Text()
}
func TestMongoBoundInitializationDoesNotImportDefaults(t *testing.T) {
	store, db, scope := mongoFixture(t)
	p := newPackFixture(t).pack(t)
	dir := t.TempDir()
	if err := app.InitializeOwned(dir, scope, store, nil); err != nil {
		t.Fatal(err)
	}
	units, err := app.NewUnitLibraryWithStore(dir, store.Units(scope))
	if err != nil {
		t.Fatal(err)
	}
	units.SetDefaults(p)
	if len(units.Units()) != 1 {
		t.Fatal("plugin defaults not visible")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, name := range []string{"maps", "units"} {
		count, err := db.Collection(name).CountDocuments(ctx, bson.M{})
		if err != nil || count != 0 {
			t.Fatalf("%s defaults imported: %d %v", name, count, err)
		}
	}
	values, err := store.LoadStatusAdditions(scope)
	if err != nil || len(values) != 0 {
		t.Fatalf("statuses imported rather than empty additions: %v", err)
	}
}
func TestMongoOwnedMapKeepsPluginArtworkReference(t *testing.T) {
	store, _, scope := mongoFixture(t)
	p := newPackFixture(t).pack(t)
	dir := t.TempDir()
	if err := app.InitializeOwned(dir, scope, store, nil); err != nil {
		t.Fatal(err)
	}
	maps, err := app.NewMapLibraryWithStore(dir, store.Maps(scope))
	if err != nil {
		t.Fatal(err)
	}
	maps.SetDefaults(p)
	copy, err := maps.SaveMap(p.Maps()[0], app.SavedBoard{Units: []app.UnitData{{Name: "Owned scout", ImageID: app.NPCUnitImageID}}})
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := app.NewMapLibraryWithStore(dir, store.Maps(scope))
	if err != nil {
		t.Fatal(err)
	}
	reloaded.SetDefaults(p)
	got, err := reloaded.LoadMap(copy.ID)
	if err != nil || got.Artwork == nil || got.Board.Units[0].Name != "Owned scout" {
		t.Fatalf("owned copy round-trip: %v", err)
	}
	if len(reloaded.Maps()) != 2 {
		t.Fatal("default replaced by owned document")
	}
	if err := store.DeleteScope(scope); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal("scoped deletion affected plugin files", err)
	}
}
