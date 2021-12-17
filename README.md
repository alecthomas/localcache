# Local file-based atomic cache manager

I repeatedly find myself writing partitioned keyed caching systems, akin to Go's module cache. To DRY myself I created
this.

It provides:

- Partitioned cache entries: `<root>/<partition>/<key>`
- Atomic creation, replacement and deletion of single files.
- Atomic creation, replacement and deletion of directory hierarchies.

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

## Implementation

The cache manager maintains transactionality/atomicity by relying on two aspects of Unix filesystems:

1. File renames are atomic.
2. Symlinks can be atomically overwritten by a rename.

The process is then:

1. Create a file or directory `F = <partition>/<hash>.<timestamp>`.
2. User writes to the file or populates the directory.
3. Create a symlink `L = <partition>/<hash>.<timestamp> -> F`
4. Rename `L` to `<partition>/<hash>`, the final "committed" name for the entry.

eg.

<table>
<tr>
<th>Code</th>
<th>Filesystem</th>
</tr>
<tr>
<td>

```go
tx, f, err := cache.Create("my-key")
f.WriteString("hello")
f.Close()
```

</td>
<td>

```
5e/5e78…f732.67e7996297ee
```

</td>
</tr>
<tr>
<td>

Step 1

```go
cache.Commit(tx)
```

</td>
<td>

```
5e/5e78…f732.67e799629823 -> 5e78…f732.67e7996297ee
5e/5e78…f732.67e7996297ee
```

</td>
</tr>
<tr>
<td>

Step 2

```go
cache.Commit(tx)
```

</td>
<td>

```
5e/5e78…f732 -> 5e78…f732.67e7996297ee
5e/5e78…f732.67e7996297ee
```

</td>
</tr>
</table>
