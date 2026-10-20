package tile

import (
	"container/list"
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	defaultMemoryCap   = 256
	defaultConcurrency = 6
)

// Options configures a Loader. The zero value is usable.
type Options struct {
	// MemoryCap is the number of decoded tiles kept in memory. Zero means
	// 256 (about 64 MB of RGBA at 256×256); negative disables the memory
	// cache.
	MemoryCap int

	// Dir is a directory for the on-disk cache of raw tile bytes, used
	// for sources that implement DataSource. Empty disables it.
	// DefaultDir returns a per-user location on native.
	Dir string

	// Concurrency bounds simultaneous fetches across every caller, so a
	// background prefetch cannot starve the render on screen. Zero means
	// 6, matching a browser's per-host connection limit.
	Concurrency int
}

// Loader fetches tiles from any Source with a memory cache, an optional
// disk cache, and one fetch per tile no matter how many callers want it.
// It is safe for concurrent use.
type Loader struct {
	dir  string
	conc int
	sem  chan struct{}

	mu       sync.Mutex
	mem      *lru
	inflight map[cacheKey]*download
}

type cacheKey struct {
	source string
	id     ID
}

type download struct {
	waiters int
	cancel  context.CancelFunc
	done    chan struct{}
	img     image.Image
	err     error
}

// NewLoader returns a Loader configured by o.
func NewLoader(o Options) *Loader {
	l := &Loader{dir: o.Dir, conc: o.Concurrency, inflight: make(map[cacheKey]*download)}
	if l.conc <= 0 {
		l.conc = defaultConcurrency
	}
	l.sem = make(chan struct{}, l.conc)
	switch {
	case o.MemoryCap == 0:
		l.mem = newLRU(defaultMemoryCap)
	case o.MemoryCap > 0:
		l.mem = newLRU(o.MemoryCap)
	}
	return l
}

// Cached returns the tile if it is in memory. It never touches the disk
// cache or the source, so it is cheap enough for a synchronous render.
func (l *Loader) Cached(src Source, id ID) (image.Image, bool) {
	if l.mem == nil || src == nil {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.mem.get(cacheKey{src.Info().Name, id})
}

// Fetch returns the tile from memory, disk, or the source. Concurrent
// calls for the same tile share one fetch; canceling ctx abandons the
// wait, and the fetch itself stops only when no caller still wants it.
func (l *Loader) Fetch(ctx context.Context, src Source, id ID) (image.Image, error) {
	if !Fetchable(src) {
		return nil, ErrNoSource
	}
	k := cacheKey{src.Info().Name, id}

	l.mu.Lock()
	if l.mem != nil {
		if img, ok := l.mem.get(k); ok {
			l.mu.Unlock()
			return img, nil
		}
	}
	d, ok := l.inflight[k]
	if ok {
		d.waiters++
	} else {
		dctx, cancel := context.WithCancel(context.Background())
		d = &download{waiters: 1, cancel: cancel, done: make(chan struct{})}
		l.inflight[k] = d
		go l.run(dctx, d, k, src, id)
	}
	l.mu.Unlock()

	select {
	case <-d.done:
		if errors.Is(d.err, context.Canceled) && ctx.Err() == nil {
			// Joined a fetch in the instant between its last waiter
			// leaving and its bookkeeping clearing; start over.
			return l.Fetch(ctx, src, id)
		}
		return d.img, d.err
	case <-ctx.Done():
		l.mu.Lock()
		d.waiters--
		if d.waiters == 0 {
			d.cancel()
		}
		l.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (l *Loader) run(ctx context.Context, d *download, k cacheKey, src Source, id ID) {
	var img image.Image
	var err error
	select {
	case l.sem <- struct{}{}:
		img, err = l.load(ctx, src, id)
		<-l.sem
	case <-ctx.Done():
		err = ctx.Err()
	}
	l.mu.Lock()
	if err == nil && l.mem != nil {
		l.mem.put(k, img)
	}
	delete(l.inflight, k)
	d.img, d.err = img, err
	l.mu.Unlock()
	d.cancel()
	close(d.done)
}

func (l *Loader) load(ctx context.Context, src Source, id ID) (image.Image, error) {
	ds, _ := src.(DataSource)
	path := l.diskPath(src, id)
	if ds == nil || path == "" {
		return src.Tile(ctx, id)
	}
	if data, err := os.ReadFile(path); err == nil {
		if img, err := Decode(data); err == nil {
			return img, nil
		}
	}
	data, err := ds.TileData(ctx, id)
	if errors.Is(err, ErrNoData) {
		return src.Tile(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	img, err := Decode(data)
	if err != nil {
		return nil, err
	}
	_ = writeFileAtomic(path, data)
	return img, nil
}

func (l *Loader) diskPath(src Source, id ID) string {
	name := src.Info().Name
	if l.dir == "" || name == "" {
		return ""
	}
	return filepath.Join(l.dir, name, strconv.Itoa(id.Z), strconv.Itoa(id.X), strconv.Itoa(id.Y))
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tile-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// FetchAll fetches every id, calling onTile for each tile as it lands.
// Fetches share the Loader's concurrency limit. Calls to onTile are
// serialized and should not block for long. It returns nil when at least
// one tile loaded or ids is empty; otherwise the first error, which is
// ctx.Err() when the context ended first.
func (l *Loader) FetchAll(ctx context.Context, src Source, ids []ID, onTile func(ID, image.Image)) error {
	if len(ids) == 0 {
		return nil
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		loaded   int
		firstErr error
	)
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(id ID) {
			defer wg.Done()
			img, err := l.Fetch(ctx, src, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			loaded++
			if onTile != nil {
				onTile(id, img)
			}
		}(id)
	}
	wg.Wait()
	if loaded > 0 {
		return nil
	}
	if firstErr == nil {
		firstErr = ctx.Err()
	}
	return firstErr
}

type lru struct {
	cap     int
	entries map[cacheKey]*list.Element
	order   *list.List // front is most recently used
}

type lruEntry struct {
	key cacheKey
	img image.Image
}

func newLRU(cap int) *lru {
	return &lru{cap: cap, entries: make(map[cacheKey]*list.Element), order: list.New()}
}

func (c *lru) get(k cacheKey) (image.Image, bool) {
	e, ok := c.entries[k]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*lruEntry).img, true
}

func (c *lru) put(k cacheKey, img image.Image) {
	if e, ok := c.entries[k]; ok {
		e.Value.(*lruEntry).img = img
		c.order.MoveToFront(e)
		return
	}
	c.entries[k] = c.order.PushFront(&lruEntry{key: k, img: img})
	for c.order.Len() > c.cap {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.entries, last.Value.(*lruEntry).key)
	}
}
