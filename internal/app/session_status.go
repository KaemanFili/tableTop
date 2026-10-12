package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// StatusStore persists definitions independently for each tabletop.
type StatusStore interface {
	LoadStatuses(string, []UnitStatusOption) ([]UnitStatusOption, error)
	InitializeStatuses(string, []UnitStatusOption) error
}
type statusDocument struct {
	ID      string             `bson:"_id" json:"-"`
	Version int                `bson:"schemaVersion" json:"schemaVersion"`
	Options []UnitStatusOption `bson:"options" json:"options"`
}

func ValidateUnitStatuses(options []UnitStatusOption) error { return validateUnitStatuses(options) }
func DefaultUnitStatuses() []UnitStatusOption               { return defaultUnitStatuses() }

func (s *MongoStore) InitializeStatuses(scope string, values []UnitStatusOption) error {
	if err := validateUnitStatuses(values); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := "unit_statuses/" + scope
	_, err := s.db.Collection("settings").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$setOnInsert": statusDocument{id, 1, values}}, options.UpdateOne().SetUpsert(true))
	return err
}
func (s *MongoStore) LoadStatuses(scope string, fallback []UnitStatusOption) ([]UnitStatusOption, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	collection := s.db.Collection("settings")
	var doc statusDocument
	err := collection.FindOne(ctx, bson.M{"_id": "unit_statuses/" + scope}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Legacy global settings are copied once, including operator customizations.
		var legacy statusDocument
		err = collection.FindOne(ctx, bson.M{"_id": "unit_statuses"}).Decode(&legacy)
		if err == nil {
			if legacy.Version != 1 {
				return nil, errors.New("unsupported legacy statuses schema")
			}
			fallback = legacy.Options
		} else if !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, err
		}
		if err := s.InitializeStatuses(scope, fallback); err != nil {
			return nil, err
		}
		err = collection.FindOne(ctx, bson.M{"_id": "unit_statuses/" + scope}).Decode(&doc)
	}
	if err != nil {
		return nil, err
	}
	if doc.Version != 1 {
		return nil, errors.New("unsupported statuses schema")
	}
	if err := validateUnitStatuses(doc.Options); err != nil {
		return nil, err
	}
	return doc.Options, nil
}

func LoadSessionStatuses(dir, scope string, store LibraryStore, limits *StorageLimits) ([]UnitStatusOption, error) {
	fallback := defaultUnitStatuses()
	path := filepath.Join(dir, "statuses.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var doc statusDocument
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		if doc.Version != 1 {
			return nil, errors.New("unsupported statuses schema")
		}
		fallback = doc.Options
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := validateUnitStatuses(fallback); err != nil {
		return nil, err
	}
	if db, ok := store.(StatusStore); ok {
		return db.LoadStatuses(scope, fallback)
	}
	if data == nil {
		limits.lock()
		defer limits.unlock()
		if err := writeJSONFile(path, statusDocument{Version: 1, Options: fallback}, limits); err != nil {
			return nil, err
		}
	}
	return slices.Clone(fallback), nil
}
func (s *AppState) UnitStatuses() []UnitStatusOption { return slices.Clone(s.statuses) }
func (s *AppState) StatusColor(status UnitStatus) string {
	for _, option := range s.statuses {
		if option.Value == status {
			return option.Color
		}
	}
	return "transparent"
}
func NewStateWithStatuses(values []UnitStatusOption) (*AppState, error) {
	if err := validateUnitStatuses(values); err != nil {
		return nil, err
	}
	s := NewState()
	s.statuses = slices.Clone(values)
	return s, nil
}

// WriteContentFile serializes quota admission across plugin staging and libraries.
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
