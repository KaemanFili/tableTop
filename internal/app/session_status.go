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
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type statusDocument struct {
	ID      string             `bson:"_id" json:"-"`
	Version int                `bson:"schemaVersion" json:"schemaVersion"`
	Options []UnitStatusOption `bson:"options" json:"options"`
}

func ValidateUnitStatuses(options []UnitStatusOption) error { return validateUnitStatuses(options) }
func DefaultUnitStatuses() []UnitStatusOption               { return defaultUnitStatuses() }

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
	LoadStatusAdditions(string) ([]UnitStatusOption, error)
}

func (s *MongoStore) LoadStatusAdditions(scope string) ([]UnitStatusOption, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := "unit_statuses/" + scope
	collection := s.db.Collection("settings")
	_, err := collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$setOnInsert": statusDocument{id, 2, []UnitStatusOption{}}}, options.UpdateOne().SetUpsert(true))
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
func validateAdditions(values []UnitStatusOption) error {
	if len(values) == 0 {
		return nil
	}
	return validateUnitStatuses(values)
}
func LoadStatusAdditions(dir, scope string, store LibraryStore, limits *StorageLimits) ([]UnitStatusOption, error) {
	if db, ok := store.(StatusAdditionsStore); ok {
		return db.LoadStatusAdditions(scope)
	}
	path := filepath.Join(dir, "statuses.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := WriteContentJSON(path, statusDocument{Version: 2, Options: []UnitStatusOption{}}, limits); err != nil {
			return nil, err
		}
		return []UnitStatusOption{}, nil
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
func EffectiveStatuses(dir, scope string, store LibraryStore, limits *StorageLimits, defaults PluginDefaults) ([]UnitStatusOption, error) {
	if err := defaults.Validate(); err != nil {
		return nil, err
	}
	additions, err := LoadStatusAdditions(dir, scope, store, limits)
	if err != nil {
		return nil, err
	}
	values := append(defaults.Statuses(), additions...)
	return values, validateUnitStatuses(values)
}
