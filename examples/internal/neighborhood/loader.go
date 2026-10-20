// Package neighborhood owns sample-app loading and caching, outside the library API.
package neighborhood

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"nimbleterminal.dev/ntmaps/internal/city"
)

const maxCacheBytes = 32 << 20
const maxEntries = 8
const maxAge = 7 * 24 * time.Hour

type FetchFunc func(context.Context, city.Bounds) (city.Dataset, error)
type Result struct {
	Data   city.Dataset
	Scene  city.Scene
	Cached bool
}
type entry struct {
	result  Result
	used    uint64
	expires time.Time
	bytes   int
}
type Loader struct {
	Fetch FetchFunc
	// Dir is optional. Empty disables disk storage (as in the browser demo).
	Dir     string
	mu      sync.Mutex
	entries map[string]entry
	tick    uint64
}

func Key(c city.Coordinate, radius float64) string {
	return fmt.Sprintf("%.5f,%.5f/%.1f", c.Lat, c.Lon, radius)
}
func (l *Loader) put(key string, result Result, sourceBytes int, expires time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tick++
	if l.entries == nil {
		l.entries = make(map[string]entry)
	}
	size := sourceBytes
	for _, b := range result.Scene.Buildings {
		size += len(b.Mesh.Vertices)*64 + len(b.Mesh.Indices)*4
	}
	size += len(result.Scene.Roads) * 96
	if size > maxCacheBytes {
		return
	}
	// Cached results need metadata, not a second copy of all source footprints.
	result.Data.Buildings = nil
	result.Data.Roads = nil
	l.entries[key] = entry{result, l.tick, expires, size}
	for {
		total := 0
		oldest := ""
		stamp := ^uint64(0)
		for k, e := range l.entries {
			total += e.bytes
			if e.used < stamp {
				oldest, stamp = k, e.used
			}
		}
		if len(l.entries) <= maxEntries && total <= maxCacheBytes {
			break
		}
		delete(l.entries, oldest)
	}
}

// Seed installs a deterministic offline neighborhood. Seed data expires only
// when this loader is discarded, but normal LRU eviction applies. Explicit refresh bypasses it.
func (l *Loader) Seed(center city.Coordinate, radius float64, d city.Dataset) error {
	s, err := city.Build(d, city.DefaultHeightPolicy())
	if err != nil {
		return err
	}
	if len(s.Buildings) == 0 {
		return fmt.Errorf("no supported buildings")
	}
	l.put(Key(center, radius), Result{Data: d, Scene: s}, 0, time.Time{})
	return nil
}

// covering returns a live entry whose data covers center, so a view that
// moved a little, after a wheel zoom around the cursor say, keeps using
// the neighborhood already on hand. The caller holds l.mu.
func (l *Loader) covering(center city.Coordinate) (entry, bool) {
	now := time.Now()
	for key, e := range l.entries {
		if !e.expires.IsZero() && now.After(e.expires) {
			continue
		}
		b := e.result.Data.Bounds
		if center.Lat >= b.South && center.Lat <= b.North && center.Lon >= b.West && center.Lon <= b.East {
			l.tick++
			e.used = l.tick
			l.entries[key] = e
			return e, true
		}
	}
	return entry{}, false
}

func (l *Loader) Load(ctx context.Context, center city.Coordinate, radius float64, refresh bool) (Result, error) {
	bounds, err := city.Around(center, radius)
	if err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	key := Key(center, radius)
	if !refresh {
		l.mu.Lock()
		e, ok := l.entries[key]
		if ok && !e.expires.IsZero() && time.Now().After(e.expires) {
			delete(l.entries, key)
			ok = false
		}
		if ok {
			l.tick++
			e.used = l.tick
			l.entries[key] = e
		}
		if !ok {
			e, ok = l.covering(center)
		}
		l.mu.Unlock()
		if ok {
			r := e.result
			r.Cached = true
			return r, nil
		}
	}
	var d city.Dataset
	saved := time.Now()
	cached := false
	if !refresh {
		if doc, err := l.readDisk(key); err == nil {
			d, saved, cached = doc.Data, doc.Saved, true
		}
	}
	if !cached {
		if l.Fetch == nil {
			return Result{}, fmt.Errorf("neighborhood is not cached; live loading is disabled")
		}
		d, err = l.Fetch(ctx, bounds)
		if err != nil {
			return Result{}, err
		}
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	s, err := city.Build(d, city.DefaultHeightPolicy())
	if err != nil {
		return Result{}, err
	}
	if len(s.Buildings) == 0 {
		return Result{}, fmt.Errorf("no supported building footprints in this neighborhood")
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	if d.Name == "" {
		d.Name = fmt.Sprintf("%.5f, %.5f", center.Lat, center.Lon)
	}
	result := Result{d, s, cached}
	encoded, _ := json.Marshal(diskEntry{1, key, saved, d})
	l.put(key, result, len(encoded), saved.Add(maxAge))
	if !cached && len(encoded) <= maxCacheBytes {
		l.writeDisk(ctx, key, encoded)
	}
	// Only retain source metadata in the returned result as well.
	result.Data.Buildings = nil
	result.Data.Roads = nil
	return result, nil
}

type diskEntry struct {
	Version int
	Key     string
	Saved   time.Time
	Data    city.Dataset
}

func filename(key string) string { return fmt.Sprintf("%x.json", sha256.Sum256([]byte(key))) }
func (l *Loader) readDisk(key string) (diskEntry, error) {
	var d diskEntry
	if l.Dir == "" {
		return d, os.ErrNotExist
	}
	f, err := os.Open(filepath.Join(l.Dir, filename(key)))
	if err != nil {
		return d, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxCacheBytes+1))
	if err != nil {
		return d, err
	}
	if len(raw) > maxCacheBytes {
		return d, fmt.Errorf("cache entry too large")
	}
	if err = json.Unmarshal(raw, &d); err != nil {
		return d, err
	}
	age := time.Since(d.Saved)
	if d.Version != 1 || d.Key != key || age < 0 || age > maxAge {
		return d, fmt.Errorf("stale cache entry")
	}
	return d, nil
}
func (l *Loader) writeDisk(ctx context.Context, key string, raw []byte) {
	if l.Dir == "" || ctx.Err() != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// Cache storage is optional: permission/space failures must not hide a scene
	// that has already loaded successfully.
	if os.MkdirAll(l.Dir, 0700) != nil {
		return
	}
	f, err := os.CreateTemp(l.Dir, ".neighborhood-*")
	if err != nil {
		return
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(raw)
	closeErr := f.Close()
	if err != nil || closeErr != nil || ctx.Err() != nil {
		return
	}
	if os.Rename(name, filepath.Join(l.Dir, filename(key))) != nil {
		return
	}
	files, _ := os.ReadDir(l.Dir)
	type savedFile struct {
		path string
		at   time.Time
		size int64
	}
	var saved []savedFile
	var total int64
	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".json" || len(f.Name()) != 69 {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		saved = append(saved, savedFile{filepath.Join(l.Dir, f.Name()), info.ModTime(), info.Size()})
		total += info.Size()
	}
	sort.Slice(saved, func(i, j int) bool { return saved[i].at.Before(saved[j].at) })
	for i, f := range saved {
		if len(saved)-i > maxEntries || total > maxCacheBytes || time.Since(f.at) > maxAge {
			if os.Remove(f.path) == nil {
				total -= f.size
			}
		}
	}
}
