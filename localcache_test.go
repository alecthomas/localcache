package localcache

import (
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

	dir, err := cache.Mkdir("test")
	require.NoError(t, err)
	_, err = cache.Finalise(dir)
	require.NoError(t, err)

	f, err := cache.Open("test")
	require.NoError(t, err)
	_ = f.Close()

	f, err = cache.Create("test")
	require.NoError(t, err)
	_, err = f.WriteString("hello")
	require.NoError(t, err)
	_ = f.Close()
	_, err = cache.Finalise(f.Name())
	require.NoError(t, err)

	f, err = cache.Open("test")
	require.NoError(t, err)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "hello", string(data))

	err = cache.Purge(time.Hour)
	require.NoError(t, err)

	err = cache.Remove("test")
	require.NoError(t, err)

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
