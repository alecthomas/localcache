package localcache

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCache(t *testing.T) {
	cache := NewForTesting(t)

	tx, _, err := cache.Mkdir("test")
	require.NoError(t, err)
	_, err = cache.Commit(tx)
	require.NoError(t, err)

	f, err := cache.Open("test")
	require.NoError(t, err)
	_ = f.Close()

	tx, f, err = cache.Create("test")
	require.NoError(t, err)
	_, err = f.WriteString("hello")
	_ = f.Close()
	require.NoError(t, err)
	_, err = cache.Commit(tx)
	require.NoError(t, err)

	f, err = cache.Open("test")
	require.NoError(t, err)
	data, err := io.ReadAll(f)
	_ = f.Close()
	require.NoError(t, err)
	require.Equal(t, "hello", string(data))

	tx, f, err = cache.Create("test-rollback")
	require.NoError(t, err)
	_ = f.Close()
	err = cache.Rollback(tx)
	require.NoError(t, err)

	err = cache.Purge(time.Hour)
	require.NoError(t, err)

	err = cache.Remove("test")
	require.NoError(t, err)

	require.Equal(t, []string{"", "/8b", "/9f"}, list(cache))
}

func TestRollbackOnError(t *testing.T) {
	cache := NewForTesting(t)
	tx, _, err := cache.Mkdir("test")
	require.NoError(t, err)
	err = func() (err error) {
		defer cache.RollbackOnError(tx, &err)
		return fmt.Errorf("function failed")
	}()
	require.Error(t, err)
	require.Equal(t, []string{"", "/9f"}, list(cache))
}

func list(cache *Cache) (out []string) {
	_ = filepath.Walk(cache.root, func(path string, info fs.FileInfo, err error) error {
		out = append(out, strings.TrimPrefix(path, cache.root))
		return nil
	})
	sort.Strings(out)
	return
}
