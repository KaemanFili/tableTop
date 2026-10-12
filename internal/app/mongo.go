package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const maxMongoDocumentBytes = 16 << 20

// MongoStore owns one shared connection pool. Artwork and live board state
// remain local, so each data directory still requires a single app process.
type MongoStore struct {
	client *mongo.Client
	db     *mongo.Database
}

func NewMongoStore(ctx context.Context, uri, database string) (*MongoStore, error) {
	if uri == "" || database == "" {
		return nil, errors.New("MongoDB URI and database are required")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetTimeout(10 * time.Second).SetWriteConcern(writeconcern.Majority()))
	if err != nil {
		return nil, err
	}
	store := &MongoStore{client: client, db: client.Database(database)}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		client.Disconnect(context.Background())
		return nil, err
	}
	for _, name := range []string{"maps", "units"} {
		_, err := store.db.Collection(name).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "scope", Value: 1}}})
		if err != nil {
			client.Disconnect(context.Background())
			return nil, err
		}
	}
	return store, nil
}

func (s *MongoStore) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }

func (s *MongoStore) Maps(scope string) DocumentStore[MapDefinition] {
	return &mongoDocumentStore[MapDefinition]{s, "maps", scope, func(m MapDefinition) string { return m.ID }}
}

func (s *MongoStore) Units(scope string) DocumentStore[UnitDefinition] {
	return &mongoDocumentStore[UnitDefinition]{s, "units", scope, func(u UnitDefinition) string { return u.ID }}
}

// Cleanup is idempotent: a tabletop remains marked for deletion until all
// scoped documents, initialization markers, and local files are removed.
func (s *MongoStore) DeleteScope(scope string) error {
	if !tableIDPattern.MatchString(scope) {
		return errors.New("invalid tabletop ID")
	}
	for _, name := range []string{"maps", "units"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := s.db.Collection(name).DeleteMany(ctx, bson.M{"scope": scope})
		cancel()
		if err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.db.Collection("settings").DeleteOne(ctx, bson.M{"_id": "unit_statuses/" + scope}); err != nil {
		return err
	}
	_, err := s.db.Collection("library_initialization").DeleteMany(ctx, bson.M{"_id": bson.M{"$in": []string{"maps/" + scope, "units/" + scope}}})
	return err
}

type mongoDocumentStore[T any] struct {
	store       *MongoStore
	name, scope string
	id          func(T) string
}

type mongoDocument[T any] struct {
	Key        string `bson:"_id"`
	Scope      string `bson:"scope"`
	Version    int    `bson:"schemaVersion"`
	Definition T      `bson:"definition"`
}

func (c *mongoDocumentStore[T]) collection() *mongo.Collection {
	return c.store.db.Collection(c.name)
}
func (c *mongoDocumentStore[T]) marker() bson.M { return bson.M{"_id": c.name + "/" + c.scope} }

func (c *mongoDocumentStore[T]) Load() ([]T, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var marker struct {
		Version int `bson:"schemaVersion"`
	}
	err := c.store.db.Collection("library_initialization").FindOne(ctx, c.marker()).Decode(&marker)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if marker.Version != 1 {
		return nil, false, errors.New("unsupported MongoDB library schema")
	}
	cursor, err := c.collection().Find(ctx, bson.M{"scope": c.scope})
	if err != nil {
		return nil, false, err
	}
	defer cursor.Close(ctx)
	entries := make([]T, 0)
	for cursor.Next(ctx) {
		var document mongoDocument[T]
		if err := cursor.Decode(&document); err != nil {
			return nil, false, err
		}
		if document.Version != 1 || document.Key != c.scope+"/"+c.id(document.Definition) {
			return nil, false, errors.New("invalid MongoDB definition document")
		}
		entries = append(entries, document.Definition)
	}
	return entries, true, cursor.Err()
}

func (c *mongoDocumentStore[T]) encode(entry T) ([]byte, error) {
	data, err := bson.Marshal(mongoDocument[T]{c.scope + "/" + c.id(entry), c.scope, 1, entry})
	if err != nil {
		return nil, err
	}
	if len(data) > maxMongoDocumentBytes {
		return nil, fmt.Errorf("%w: saved %s document exceeds MongoDB's 16 MiB limit", ErrCapacity, c.name)
	}
	return data, nil
}

// Imports use setOnInsert and publish the marker last. Interrupted imports can
// be retried; once initialized, an empty library never resurrects JSON entries.
func (c *mongoDocumentStore[T]) Initialize(entries []T) error {
	// Preflight every document before any database write.
	documents := make([]bson.Raw, len(entries))
	for i, entry := range entries {
		data, err := c.encode(entry)
		if err != nil {
			return err
		}
		documents[i] = data
	}
	for i, entry := range entries {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := c.collection().UpdateOne(ctx, bson.M{"_id": c.scope + "/" + c.id(entry)}, bson.M{"$setOnInsert": documents[i]}, options.UpdateOne().SetUpsert(true))
		cancel()
		if err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := c.store.db.Collection("library_initialization").UpdateOne(ctx, c.marker(), bson.M{"$setOnInsert": bson.M{"schemaVersion": 1}}, options.UpdateOne().SetUpsert(true))
	return err
}

// Replacing a map document commits its metadata and embedded board together.
func (c *mongoDocumentStore[T]) Put(entry T) error {
	data, err := c.encode(entry)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = c.collection().ReplaceOne(ctx, bson.M{"_id": c.scope + "/" + c.id(entry)}, bson.Raw(data), options.Replace().SetUpsert(true))
	return err
}

func (c *mongoDocumentStore[T]) Delete(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := c.collection().DeleteOne(ctx, bson.M{"_id": c.scope + "/" + id})
	return err
}

func (c *mongoDocumentStore[T]) Exists(id string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	collection := c.store.db.Collection(c.name, options.Collection().SetReadPreference(readpref.Primary()).SetReadConcern(readconcern.Majority()))
	err := collection.FindOne(ctx, bson.M{"_id": c.scope + "/" + id}, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return err == nil, err
}

// LoadUnitStatuses seeds the server-wide status document on first use, then
// uses its ordered definitions for validation, the picker, and token colors.
func (s *MongoStore) LoadUnitStatuses(ctx context.Context) error {
	type statusDocument struct {
		ID      string             `bson:"_id"`
		Version int                `bson:"schemaVersion"`
		Options []UnitStatusOption `bson:"options"`
	}
	collection := s.db.Collection("settings")
	filter := bson.M{"_id": "unit_statuses"}
	_, err := collection.UpdateOne(ctx, filter, bson.M{"$setOnInsert": statusDocument{"unit_statuses", 1, defaultUnitStatuses()}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return err
	}
	var document statusDocument
	if err := collection.FindOne(ctx, filter).Decode(&document); err != nil {
		return err
	}
	if document.Version != 1 {
		return errors.New("unsupported status definitions schema")
	}
	return setUnitStatuses(document.Options)
}
