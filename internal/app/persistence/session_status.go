package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"tableTop/main/internal/app/model"
)

type statusDocument struct {
	ID      string                   `bson:"_id" json:"-"`
	Version int                      `bson:"schemaVersion" json:"schemaVersion"`
	Options []model.UnitStatusOption `bson:"options" json:"options"`
}

// WriteContentFile serializes quota admission across owned libraries.
func WriteContentFile(path string, data []byte, limits *StorageLimits) error {
	limits.lock()
	defer limits.unlock()
	return writeStorageFile(path, data, 0600, limits)
}
func WriteContentJSON(path string, value any, limits *StorageLimits) error {
	limits.lock()
	defer limits.unlock()
	return writeJSONFile(path, value, limits)
}

// Status additions are explicitly distinct from a complete default list.
type StatusAdditionsStore interface {
	LoadStatusAdditions(string) ([]model.UnitStatusOption, error)
}

func (s *MongoStore) LoadStatusAdditions(scope string) ([]model.UnitStatusOption, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := "unit_statuses/" + scope
	collection := s.db.Collection("settings")
	_, err := collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$setOnInsert": statusDocument{id, 2, []model.UnitStatusOption{}}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	var doc statusDocument
	if err := collection.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		return nil, err
	}
	if doc.Version != 2 {
		return nil, errors.New("expected a status additions document")
	}
	return doc.Options, validateAdditions(doc.Options)
}
func validateAdditions(values []model.UnitStatusOption) error {
	if len(values) == 0 {
		return nil
	}
	return model.ValidateUnitStatuses(values)
}
func LoadStatusAdditions(dir, scope string, store LibraryStore, limits *StorageLimits) ([]model.UnitStatusOption, error) {
	if db, ok := store.(StatusAdditionsStore); ok {
		return db.LoadStatusAdditions(scope)
	}
	path := filepath.Join(dir, "statuses.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := WriteContentJSON(path, statusDocument{Version: 2, Options: []model.UnitStatusOption{}}, limits); err != nil {
			return nil, err
		}
		return []model.UnitStatusOption{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc statusDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Version != 2 {
		return nil, errors.New("expected a status additions document")
	}
	return doc.Options, validateAdditions(doc.Options)
}
func EffectiveStatuses(dir, scope string, store LibraryStore, limits *StorageLimits, defaults PluginDefaults) ([]model.UnitStatusOption, error) {
	if err := defaults.Validate(); err != nil {
		return nil, err
	}
	additions, err := LoadStatusAdditions(dir, scope, store, limits)
	if err != nil {
		return nil, err
	}
	values := append(defaults.Statuses(), additions...)
	return values, model.ValidateUnitStatuses(values)
}
