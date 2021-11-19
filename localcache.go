package localcache

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Transaction key for an uncommitted cache entry.
type Transaction string

func (t Transaction) path(root string) string { return filepath.Join(root, string(t)[:2], string(t)) }

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
	if err != nil {
		return nil, fmt.Errorf("couldn't create cache dir: %w", err)
	}
	return &Cache{root}, nil
}

// Commit atomically commits an in-flight file or directory creation Transaction to the Cache.
func (c *Cache) Commit(key Transaction) (string, error) {
	path := key.path(c.root)
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
	tmpSymlink := fmt.Sprintf("%s.%x", dest, time.Now().UnixNano())
	err = os.Symlink(path, tmpSymlink)
	if err != nil {
		return "", fmt.Errorf("failed to finalise: %w", err)
	}

	// Then atomically rename the new symlink to the final destination symlink.
	err = os.Rename(tmpSymlink, dest)
	if err != nil {
		return "", fmt.Errorf("failed to finalise: %w", err)
	}
	if oldDest != "" {
		_ = os.RemoveAll(oldDest)
	}
	return dest, nil
}

// Rollback reverts an in-flight file or directory creation Transaction.
func (c *Cache) Rollback(key Transaction) error {
	path := key.path(c.root)
	return os.RemoveAll(path)
}

// Mkdir creates a directory in the cache.
//
// Commit() must be called with the returned path to atomically
// add the created directory to the Cache.
//
//     dir, err := cache.Mkdir("my-key")
//     err = f.Close()
//     err = cache.Commit(dir)
func (c *Cache) Mkdir(key string) (Transaction, string, error) {
	path, err := c.preparePath(key)
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
// Commit() must be called with the returned os.File's path to atomically
// add the created file to the Cache.
//
//     f, err := cache.Create("my-key")
//     err = f.Close()
//     err = cache.Commit(f.Name())
func (c *Cache) Create(key string) (Transaction, *os.File, error) {
	path, err := c.preparePath(key)
	if err != nil {
		return "", nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return "", nil, fmt.Errorf("could not create cache directory: %w", err)
	}
	return Transaction(filepath.Base(path)), f, nil
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

// Path returns the path to a cache entry if it exists.
func (c *Cache) Path(key string) (string, error) {
	key = hash(key, false)
	path := filepath.Join(c.root, key[:2], key)
	_, err := os.Stat(path)
	return path, err
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

// Purge all entries older than the given age.
func (c *Cache) Purge(older time.Duration) error {
	return filepath.Walk(c.root, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		ext := filepath.Ext(path)
		if ext == "" {
			return nil
		}
		hexTimestamp := strings.TrimPrefix(ext, ".")
		var ts int64
		_, err = fmt.Sscanf(hexTimestamp, "%x", &ts)
		if err != nil {
			return fmt.Errorf("invalid cache entry %q: %w", path, err)
		}
		fileTime := time.Unix(0, ts)
		if time.Since(fileTime) < older {
			return nil
		}
		err = os.Remove(strings.TrimSuffix(path, ext))
		if err != nil {
			return fmt.Errorf("failed to remove entry link: %w", err)
		}
		err = os.RemoveAll(path)
		if err != nil {
			return fmt.Errorf("failed to remove entry: %w", err)
		}
		return nil
	})
}

func (c *Cache) preparePath(key string) (string, error) {
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
		return fmt.Sprintf("%x.%x", h, time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", h)
}
