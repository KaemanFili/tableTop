package app

import "errors"

// DocumentStore persists individual definitions. Initialize must be retryable and
// must distinguish a new library from one whose entries were all deleted.
type DocumentStore[T any] interface {
	Load() (entries []T, initialized bool, err error)
	Initialize([]T) error
	Put(T) error
	Delete(id string) error
	Exists(id string) (bool, error)
}

// A lost response can follow a committed delete. Only treat that error as
// success after an authoritative read confirms the document is absent.
func deleteDocument[T any](store DocumentStore[T], id string) error {
	err := store.Delete(id)
	if err == nil {
		return nil
	}
	exists, checkErr := store.Exists(id)
	if checkErr != nil {
		return errors.Join(err, checkErr)
	}
	if !exists {
		return nil
	}
	return err
}

// LibraryStore scopes definitions to a development board or tabletop ID.
type LibraryStore interface {
	Maps(scope string) DocumentStore[MapDefinition]
	Units(scope string) DocumentStore[UnitDefinition]
}
