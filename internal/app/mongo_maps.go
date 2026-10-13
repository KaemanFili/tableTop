package app

import (
	"context"
	"errors"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoMapStore struct {
	*mongoDocumentStore[MapDefinition]
}

func (c *mongoMapStore) List() ([]MapDefinition, bool, error) {
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
		return nil, false, errors.New("unsupported map schema")
	}
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"scope": c.scope}}},
		bson.D{{Key: "$set", Value: bson.M{"hasSavedBoard": bson.M{"$ne": bson.A{bson.M{"$ifNull": bson.A{"$definition.board", nil}}, nil}}}}},
		bson.D{{Key: "$project", Value: bson.M{"definition.board": 0}}},
	}
	cursor, err := c.collection().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, false, err
	}
	defer cursor.Close(ctx)
	result := []MapDefinition{}
	for cursor.Next(ctx) {
		var doc struct {
			Key        string        `bson:"_id"`
			Scope      string        `bson:"scope"`
			Version    int           `bson:"schemaVersion"`
			Definition MapDefinition `bson:"definition"`
			Has        bool          `bson:"hasSavedBoard"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, false, err
		}
		if doc.Version != 1 || doc.Key != c.scope+"/"+doc.Definition.ID {
			return nil, false, errors.New("invalid map document")
		}
		doc.Definition.HasSavedBoard = doc.Has
		result = append(result, doc.Definition)
	}
	return result, true, cursor.Err()
}
func (c *mongoMapStore) Get(id string) (MapDefinition, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var doc mongoDocument[MapDefinition]
	err := c.collection().FindOne(ctx, bson.M{"_id": c.scope + "/" + id, "scope": c.scope}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return MapDefinition{}, os.ErrNotExist
	}
	if err != nil {
		return MapDefinition{}, err
	}
	if doc.Version != 1 || doc.Definition.ID != id {
		return MapDefinition{}, errors.New("invalid map document")
	}
	return doc.Definition, nil
}
func (c *mongoMapStore) InitializeEach(visit func(func(MapDefinition) error) error) error {
	// First pass validates every generated document before publishing any.
	if err := visit(func(m MapDefinition) error { _, err := c.encode(m); return err }); err != nil {
		return err
	}
	if err := visit(func(m MapDefinition) error {
		data, err := c.encode(m)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err = c.collection().UpdateOne(ctx, bson.M{"_id": c.scope + "/" + m.ID}, bson.M{"$setOnInsert": bson.Raw(data)}, options.UpdateOne().SetUpsert(true))
		return err
	}); err != nil {
		return err
	}
	return c.mongoDocumentStore.Initialize(nil)
}
