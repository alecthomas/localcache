package localcache

import (
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Transaction key for an uncommitted cache entry.
type Transaction string

var clock clocker = realClock{}

// Valid returns true if the Transaction is valid.
func (t Transaction) Valid() bool { return t != "" }

func (t Transaction) path(root string) string {
	if !t.Valid() {
		panic("transaction is not valid")
	}
	return filepath.Join(root, string(t)[:2], string(t))
}

// Cache type.
type Cache struct{ root string }

// NewForTesting creates a new Cache for testing.
//
// The Cache will be removed on test completion.
func NewForTesting(t testing.TB) *Cache {
	root, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return &Cache{root}
}

// New creates a new cache "name" under the user's cache directory.
func New(name string) (*Cache, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("couldn't locate cache dir: %w", err)
	}
	root := filepath.Join(cacheDir, name)
	err = os.Mkdir(root, 0700)
	if err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("couldn't create cache dir: %w", err)
	}
	return &Cache{root}, nil
}

// Commit atomically commits an in-flight file or directory creation Transaction to the Cache.
func (c *Cache) Commit(tx Transaction) (string, error) {
	if !tx.Valid() {
		return "", fmt.Errorf("transaction is not valid")
	}
	path := tx.path(c.root)
	if !strings.HasPrefix(path, c.root) {
		return "", fmt.Errorf("cannot finalise path outside cache root")
	}
	dest := strings.TrimSuffix(path, filepath.Ext(path))

	// Check if the file we're committing actually exists.
	_, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	// First, store the old link if any, so we can remove its target.
	oldDest, err := os.Readlink(dest)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read link: %w", err)
	}

	// Next create a temporary symlink pointing to the new destination.
	tmpSymlink := fmt.Sprintf("%s.%x", dest, clock.Now().UnixNano())
	err = os.Symlink(path, tmpSymlink)
	if err != nil {
		return "", fmt.Errorf("failed to finalise symlink: %w", err)
	}

	// Then atomically rename the new symlink to the final destination symlink.
	err = os.Rename(tmpSymlink, dest)
	if err != nil {
		return "", fmt.Errorf("failed to finalise rename: %w", err)
	}
	if oldDest != "" {
		_ = os.RemoveAll(oldDest)
	}
	return dest, nil
}

// Rollback reverts an in-flight file or directory creation Transaction.
func (c *Cache) Rollback(tx Transaction) error {
	if !tx.Valid() {
		return fmt.Errorf("transaction is not valid")
	}
	path := tx.path(c.root)
	return os.RemoveAll(path)
}

// RollbackOnError is a convenience method for use with defer.
//
// It will Rollback on error, however Commit must be called manually.
//
//     defer cache.RollbackOnError(tx, &err)
func (c *Cache) RollbackOnError(tx Transaction, err *error) {
	if *err != nil {
		rberr := c.Rollback(tx)
		if rberr != nil {
			*err = fmt.Errorf("error rolling back: %s: %w", rberr, *err)
		}
	}
}

// RollbackOrCommit is a convenience method for use with defer.
//
// It will Rollback on error or otherwise Commit.
//
//     defer cache.RollbackOrCommit(tx, &err)
func (c *Cache) RollbackOrCommit(tx Transaction, err *error) {
	if *err == nil {
		_, *err = c.Commit(tx)
	} else {
		rberr := c.Rollback(tx)
		if rberr != nil {
			*err = fmt.Errorf("error rolling back: %s: %w", rberr, *err)
		}
	}
}

// Mkdir creates a directory in the cache.
//
// Commit() must be called with the returned Transaction to atomically
// add the created directory to the Cache.
//
//     tx, dir, err := cache.Mkdir("my-key")
//     err = cache.Commit(tx)
func (c *Cache) Mkdir(key string) (Transaction, string, error) {
	path, err := c.pathForKey(key)
	if err != nil {
		return "", "", err
	}
	err = os.Mkdir(path, 0700)
	if err != nil {
		return "", "", fmt.Errorf("could not create cache directory: %w", err)
	}
	return Transaction(filepath.Base(path)), path, nil
}

// Create a file in the Cache.
//
// Commit() must be called with the returned Transaction to atomically
// add the created file to the Cache.
//
//     tx, f, err := cache.Create("my-key")
//     err = f.Close()
//     err = cache.Commit(tx)
func (c *Cache) Create(key string) (Transaction, *os.File, error) {
	path, err := c.pathForKey(key)
	if err != nil {
		return "", nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return "", nil, fmt.Errorf("could not create cache file: %w", err)
	}
	return Transaction(filepath.Base(path)), f, nil
}

// WriteFile writes a byte slice to a file in the cache.
func (c *Cache) WriteFile(key string, data []byte) (err error) {
	tx, w, err := c.Create(key)
	if err != nil {
		return err
	}
	defer c.RollbackOrCommit(tx, &err)
	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	err = w.Close()
	if err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}
	return nil
}

// CreateOrRead creates a key if it doesn't exist, or opens it for reading if it does.
//
// Use Transaction.Valid() to check if the key was created.
func (c *Cache) CreateOrRead(key string) (Transaction, *os.File, error) {
	f, err := c.Open(key)
	if err != nil {
		return c.Create(key)
	}
	return "", f, err
}

// Remove cache entry atomically.
func (c *Cache) Remove(key string) error {
	key = hash(key, false)
	path := filepath.Join(c.root, key[:2], key)

	// First, store the old link if any, so we can remove its target.
	oldDest, err := os.Readlink(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read entry: %w", err)
	}

	err = os.Remove(path)
	if err != nil {
		return fmt.Errorf("failed to remove cache entry: %w", err)
	}

	if oldDest != "" {
		_ = os.RemoveAll(oldDest)
	}
	return nil
}

// IfExists returns the path to a cache entry if it exists, or empty string if it does not.
func (c *Cache) IfExists(key string) string {
	key = hash(key, false)
	path := filepath.Join(c.root, key[:2], key)
	_, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return path
}

// Open a file or directory in the Cache.
func (c *Cache) Open(key string) (*os.File, error) {
	key = hash(key, false)
	return os.Open(filepath.Join(c.root, key[:2], key))
}

// ReadFile identified by key.
func (c *Cache) ReadFile(key string) ([]byte, error) {
	key = hash(key, false)
	path := filepath.Join(c.root, key[:2], key)
	return ioutil.ReadFile(path)
}

// Purge entry for given key if older than given age.
func (c *Cache) PurgeKey(key string, older time.Duration) error {
	key = hash(key, false)
	path := filepath.Join(c.root, key[:2], key)
	entry, err := os.Readlink(path)
	if err != nil && os.IsNotExist(err) {
		return nil // no entry to be purged
	}
	if err != nil {
		return fmt.Errorf("could not read link for purging: %w", err)
	}
	return removeEntry(entry, older)
}

// Purge all entries older than the given age.
func (c *Cache) Purge(older time.Duration) error {
	partitions, err := filepath.Glob(filepath.Join(c.root, "*"))
	if err != nil {
		return fmt.Errorf("could not list partitions: %w", err)
	}
	for _, partition := range partitions {
		entries, err := filepath.Glob(filepath.Join(partition, "*"))
		if err != nil {
			return fmt.Errorf("could not list entries in %q: %w", partition, err)
		}
		for _, entry := range entries {
			if err := removeEntry(entry, older); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeEntry(entry string, older time.Duration) error {
	ext := filepath.Ext(entry)
	if ext == "" {
		return nil
	}
	hexTimestamp := strings.TrimPrefix(ext, ".")
	var ts int64
	_, err := fmt.Sscanf(hexTimestamp, "%x", &ts)
	if err != nil {
		return fmt.Errorf("invalid cache entry %q: %w", entry, err)
	}
	fileTime := time.Unix(0, ts)
	if clock.Since(fileTime) < older {
		return nil
	}
	err = os.Remove(strings.TrimSuffix(entry, ext))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove entry link: %w", err)
	}
	err = os.RemoveAll(entry)
	if err != nil {
		return fmt.Errorf("failed to remove entry: %w", err)
	}
	return nil
}

func (c *Cache) pathForKey(key string) (string, error) {
	key = hash(key, true)
	path := filepath.Join(c.root, key[:2], key)
	err := os.Mkdir(filepath.Dir(path), 0700)
	if err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("failed to create cache partition: %w", err)
	}
	return path, nil
}

func hash(key string, timestamp bool) string {
	h := sha256.Sum256([]byte(key))
	if timestamp {
		return fmt.Sprintf("%x.%x", h, clock.Now().UnixNano())
	}
	return fmt.Sprintf("%x", h)
}
