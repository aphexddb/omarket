package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Purchase statuses.
const (
	StatusPending  = "pending"
	StatusComplete = "complete"
)

var purchasesBucket = []byte("purchases")

// ErrNotFound is returned when a purchase token isn't in the store.
var ErrNotFound = errors.New("server: purchase not found")

// Purchase is the record stored per purchase token.
type Purchase struct {
	App        string `json:"app"`
	Email      string `json:"email"`
	Status     string `json:"status"`
	LicenseKey string `json:"license_key,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

// Store is the bbolt-backed purchase store: bucket "purchases", key =
// token, value = JSON-encoded Purchase.
type Store struct {
	db *bolt.DB
}

// OpenStore opens (creating if necessary) the bbolt database at path and
// ensures the purchases bucket exists.
func OpenStore(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("opening store %q: %w", path, err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(purchasesBucket)
		return err
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing purchases bucket: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying bbolt database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Put stores (or overwrites) the purchase record for token.
func (s *Store) Put(token string, p Purchase) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(purchasesBucket).Put([]byte(token), data)
	})
}

// Get retrieves the purchase record for token. Returns ErrNotFound if
// token is unknown.
func (s *Store) Get(token string) (Purchase, error) {
	var p Purchase
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(purchasesBucket).Get([]byte(token))
		if v == nil {
			return ErrNotFound
		}
		return json.Unmarshal(v, &p)
	})
	return p, err
}
