package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

var ErrInvitation = errors.New("an administrator invitation is required to join this server")

type Invitation struct {
	ID      string    `json:"id"`
	Expires time.Time `json:"expires"`
}

type accessCatalog struct {
	Members []string     `json:"members"`
	Invites []Invitation `json:"invites"`
}

// Only invitation hashes are persisted. The usable link is shown once to its admin.
type AccessLibrary struct {
	mu     sync.Mutex
	path   string
	admins []string
	data   accessCatalog
	limits *StorageLimits
}

func NewAccessLibrary(dir string, admins []string, limits *StorageLimits) (*AccessLibrary, error) {
	if len(admins) == 0 {
		return nil, errors.New("set TABLETOP_ADMIN_USER_IDS to the Discord IDs of the server administrators")
	}
	for _, id := range admins {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("administrator IDs must not be empty")
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	library := &AccessLibrary{path: filepath.Join(dir, "access.json"), admins: slices.Clone(admins), limits: limits}
	data, err := os.ReadFile(library.path)
	if err == nil {
		err = json.Unmarshal(data, &library.data)
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return library, err
}

func (library *AccessLibrary) IsAdmin(id string) bool { return slices.Contains(library.admins, id) }

func (library *AccessLibrary) Allowed(id string) bool {
	library.mu.Lock()
	defer library.mu.Unlock()
	return library.IsAdmin(id) || slices.Contains(library.data.Members, id)
}

func invitationHash(code string) string {
	hash := sha256.Sum256([]byte(code))
	return hex.EncodeToString(hash[:])
}

func (library *AccessLibrary) Admit(id, code string) error {
	library.mu.Lock()
	defer library.mu.Unlock()
	if library.IsAdmin(id) || slices.Contains(library.data.Members, id) {
		return nil
	}
	if id == "" || len(library.data.Members) >= 256 {
		return ErrInvitation
	}
	for index, invite := range library.data.Invites {
		if invite.ID != invitationHash(code) || !time.Now().Before(invite.Expires) {
			continue
		}
		proposed := accessCatalog{Members: append(slices.Clone(library.data.Members), id), Invites: slices.Delete(slices.Clone(library.data.Invites), index, index+1)}
		if err := library.save(proposed); err != nil {
			return err
		}
		library.data = proposed
		return nil
	}
	return ErrInvitation
}

func (library *AccessLibrary) CreateInvitation(admin string) (string, error) {
	if !library.IsAdmin(admin) {
		return "", errors.New("only server administrators can invite players")
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	now := time.Now()
	invites := slices.DeleteFunc(slices.Clone(library.data.Invites), func(invite Invitation) bool { return !now.Before(invite.Expires) })
	if len(invites) >= 100 {
		return "", errors.New("there are already 100 pending invitations; revoke one before creating another")
	}
	code := rand.Text()
	proposed := accessCatalog{Members: library.data.Members, Invites: append(invites, Invitation{ID: invitationHash(code), Expires: now.Add(7 * 24 * time.Hour)})}
	if err := library.save(proposed); err != nil {
		return "", err
	}
	library.data = proposed
	return code, nil
}

func (library *AccessLibrary) Invitations(admin string) []Invitation {
	if !library.IsAdmin(admin) {
		return nil
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(library.data.Invites), func(invite Invitation) bool { return !time.Now().Before(invite.Expires) })
}

func (library *AccessLibrary) RevokeInvitation(admin, id string) error {
	if !library.IsAdmin(admin) {
		return errors.New("only server administrators can revoke invitations")
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	proposed := accessCatalog{Members: library.data.Members, Invites: slices.DeleteFunc(slices.Clone(library.data.Invites), func(invite Invitation) bool { return invite.ID == id })}
	if err := library.save(proposed); err != nil {
		return err
	}
	library.data = proposed
	return nil
}

func (library *AccessLibrary) save(data accessCatalog) error {
	library.limits.lock()
	defer library.limits.unlock()
	return writeJSONFile(library.path, data, library.limits)
}
