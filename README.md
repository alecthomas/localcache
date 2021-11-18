# Local cache manager

I repeatedly find myself writing partitioned keyed caching 
systems, akin to Go's module cache. To DRY myself I created this.

It provides:

- Partitioned cache entries: `<root>/<partition>/<key>`
- Atomic directory and file cache entry creation and replacement.

## Usage

```go
cache, err := localcache.New("myapp")

// Create a temporarily unaddressable file in the cache.
f, err := cache.Create("some-key")
// Write to f

// Make the file addressable.
err = cache.Finalise(f.Name())

// Load the cache entry back.
data, err := cache.ReadFile("some-key")

// Open the cache entry for reading.
data, err := cache.Open("some-key")

// Create a temporarily unaddressable directory with the same key as the previous file.
dir, err := cache.Mkdir("some-key")

// Atomically replace the previous file with the new directory.
err := cache.Finalise(dir)
```