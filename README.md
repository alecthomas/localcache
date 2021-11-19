# Local atomic cache manager

I repeatedly find myself writing partitioned keyed caching systems, akin to Go's module cache. To DRY myself I created
this.

It provides:

- Partitioned cache entries: `<root>/<partition>/<key>`
- Atomic directory and file cache entry creation and replacement.

## Usage

```go
cache, err := localcache.New("myapp")

// Create a temporarily unaddressable file in the cache.
tx, f, err := cache.Create("some-key")
// Write to f

// Commit the file to the cache.
err = cache.Commit(tx)

// Load the cache entry back.
data, err := cache.ReadFile("some-key")

// Open the cache entry for reading.
data, err := cache.Open("some-key")

// Create a temporarily unaddressable directory with the same key as the previous file.
tx, dir, err := cache.Mkdir("some-key")

// Atomically replace the previous file with the new directory.
err := cache.Commit(tx)
```