package tile

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Validators identify a stored representation for a conditional request.
type Validators struct{ ETag, LastModified string }

// TileResponse carries encoded bytes and HTTP cache headers. NotModified
// means the stored bytes are still valid; Data is then empty.
type TileResponse struct {
	Data        []byte
	Header      http.Header
	NotModified bool
}

// MetadataSource optionally extends DataSource with conditional requests and
// cache metadata. Existing DataSource implementations need no changes.
type MetadataSource interface {
	DataSource
	TileDataMeta(context.Context, ID, Validators) (TileResponse, error)
}

// DiskCachePolicy lets local sources opt out of duplicate disk storage.
type DiskCachePolicy interface{ DiskCacheable() bool }

// HTTPError reports a non-success HTTP status from a Provider.
type HTTPError struct {
	StatusCode int
	URL        string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("tile: GET %s: HTTP %d", e.URL, e.StatusCode) }

const maxCacheMeta = 16 << 10
const cacheMagic = "NTMAPS1\n"

type cacheMeta struct {
	Header  http.Header
	Expires time.Time
}

func cacheHeaders(h http.Header) http.Header {
	out := make(http.Header)
	for _, key := range []string{"Cache-Control", "Expires", "ETag", "Last-Modified", "Date", "Age"} {
		if v := h.Values(key); len(v) > 0 {
			out[http.CanonicalHeaderKey(key)] = append([]string(nil), v...)
		}
	}
	return out
}

func freshness(h http.Header, now time.Time, fallback time.Duration) (time.Time, bool) {
	ttl := fallback
	noStore, noCache := false, false
	var maxAge *time.Duration
	for _, part := range strings.Split(strings.Join(h.Values("Cache-Control"), ","), ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		switch strings.ToLower(kv[0]) {
		case "no-store":
			noStore = true
		case "no-cache":
			noCache = true
		case "max-age":
			// Cap enormous lifetimes before converting to Duration.
			ttl = 0
			if len(kv) == 2 {
				if n, err := strconv.ParseInt(strings.Trim(kv[1], " \""), 10, 64); err == nil && n >= 0 {
					ttl = time.Duration(min(n, int64((1<<63-1)/int64(time.Second)))) * time.Second
				}
			}
			v := ttl
			maxAge = &v
		}
	}
	date, err := http.ParseTime(h.Get("Date"))
	if err != nil {
		date = now
	}
	if maxAge != nil {
		ttl = *maxAge
	} else if h.Get("Expires") != "" {
		expiry, err := http.ParseTime(h.Get("Expires"))
		ttl = 0
		if err == nil {
			ttl = expiry.Sub(date)
		}
	}
	age := max(time.Duration(0), now.Sub(date))
	if n, err := strconv.ParseInt(h.Get("Age"), 10, 64); err == nil && n > 0 {
		age = max(age, time.Duration(min(n, int64((1<<63-1)/int64(time.Second))))*time.Second)
	}
	// Avoid duration underflow for a negative embedder fallback.
	if noCache || ttl <= age {
		return now, noStore
	}
	return now.Add(ttl - age), noStore
}

func (l *Loader) load(ctx context.Context, src Source, id ID) (image.Image, time.Time, error) {
	// Keep each constituent's cache policy and validators separate.
	if sources, ok := src.(firstOf); ok {
		for _, source := range sources {
			img, expires, err := l.load(ctx, source, id)
			if !errors.Is(err, ErrNotFound) {
				return img, expires, err
			}
		}
		return nil, time.Time{}, ErrNotFound
	}
	ds, ok := src.(DataSource)
	if !ok {
		img, err := src.Tile(ctx, id)
		return img, time.Time{}, err
	}
	path := l.diskPath(src, id)
	if policy, ok := src.(DiskCachePolicy); ok && !policy.DiskCacheable() {
		img, err := src.Tile(ctx, id)
		return img, time.Time{}, err
	}
	var old cacheMeta
	var data []byte
	var stale image.Image
	if path != "" {
		old, data = readCache(path)
		if len(data) > 0 {
			stale, _ = Decode(data)
		}
		if stale != nil && time.Now().Before(old.Expires) {
			l.touch(path)
			return stale, old.Expires, nil
		}
	}
	var response TileResponse
	var err error
	if ms, ok := src.(MetadataSource); ok {
		var validators Validators
		if stale != nil {
			validators = Validators{old.Header.Get("ETag"), old.Header.Get("Last-Modified")}
		}
		response, err = ms.TileDataMeta(ctx, id, validators)
	} else {
		response.Data, err = ds.TileData(ctx, id)
	}
	if errors.Is(err, ErrNoData) {
		img, err := src.Tile(ctx, id)
		return img, time.Time{}, err
	}
	if err != nil {
		var he *HTTPError
		var ue *url.Error
		var ne net.Error
		retryable := (errors.As(err, &he) && he.StatusCode >= 500 && he.StatusCode <= 599) || errors.As(err, &ue) || errors.As(err, &ne)
		if stale != nil && retryable && ctx.Err() == nil {
			// Do not touch or rewrite failed revalidations, or extend freshness.
			return stale, time.Now(), nil
		}
		return nil, time.Time{}, err
	}
	header := cacheHeaders(response.Header)
	img := stale
	if response.NotModified {
		if stale == nil {
			return nil, time.Time{}, errors.New("tile: 304 without a cached tile")
		}
		// Retain representation metadata, but Date/Age describe this response.
		merged := old.Header.Clone()
		if merged == nil {
			merged = make(http.Header)
		}
		merged.Del("Date")
		merged.Del("Age")
		for k, v := range header {
			merged[k] = v
		}
		header = merged
	} else {
		data = response.Data
		if len(data) > maxTileBytes {
			return nil, time.Time{}, errors.New("tile: encoded tile exceeds byte limit")
		}
		img, err = Decode(data)
		if err != nil {
			return nil, time.Time{}, err
		}
	}
	expires, noStore := freshness(header, time.Now(), l.ttl)
	if noStore {
		if path != "" {
			l.removeCache(path)
		}
		return img, time.Now(), nil
	}
	meta := cacheMeta{Header: header, Expires: expires}
	if path != "" {
		l.writeCache(path, meta, data)
	}
	return img, expires, nil
}

// One bounded JSON line followed by encoded image bytes; the whole record is
// renamed atomically so concurrent readers never pair old bytes with new metadata.
func readCache(path string) (cacheMeta, []byte) {
	var meta cacheMeta
	f, err := os.Open(path)
	if err != nil {
		return meta, nil
	}
	defer f.Close()
	r := bufio.NewReaderSize(io.LimitReader(f, maxTileBytes+maxCacheMeta+int64(len(cacheMagic))+2), maxCacheMeta+1)
	magic, err := r.ReadString('\n')
	if err != nil || magic != cacheMagic {
		return meta, nil
	}
	line, err := r.ReadSlice('\n')
	if err != nil || len(line) > maxCacheMeta || json.Unmarshal(line, &meta) != nil {
		return cacheMeta{}, nil
	}
	data, err := io.ReadAll(io.LimitReader(r, maxTileBytes+1))
	if err != nil || len(data) > maxTileBytes {
		return cacheMeta{}, nil
	}
	return meta, data
}

type diskEntry struct {
	path string
	size int64
	used time.Time
}
type diskCache struct {
	mu        sync.Mutex
	cap       int64
	entries   map[string]diskEntry
	total     int64
	sinceScan int64
}

// A loader scans once initially and reconciles after each 5% of capacity
// written. This bounds drift from other writers without walking per tile.
// Pressure triggers a fresh scan and batch eviction down to 90%.
func (l *Loader) scanCache() {
	d := &l.disk
	d.entries = make(map[string]diskEntry)
	d.total = 0
	d.sinceScan = 0
	_ = filepath.WalkDir(l.dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".tile-") {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return nil
		}
		d.entries[path] = diskEntry{path, info.Size(), info.ModTime()}
		d.total += info.Size()
		return nil
	})
}
func (l *Loader) touch(path string) {
	l.disk.mu.Lock()
	defer l.disk.mu.Unlock()
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	if e, ok := l.disk.entries[path]; ok {
		e.used = now
		l.disk.entries[path] = e
	}
}
func (l *Loader) removeCache(path string) {
	l.disk.mu.Lock()
	defer l.disk.mu.Unlock()
	if err := os.Remove(path); err == nil || os.IsNotExist(err) {
		if e, ok := l.disk.entries[path]; ok {
			l.disk.total -= e.size
			delete(l.disk.entries, path)
		}
	}
}
func (l *Loader) writeCache(path string, meta cacheMeta, data []byte) {
	header, err := json.Marshal(meta)
	if err != nil || len(header)+1 > maxCacheMeta {
		return
	}
	record := make([]byte, 0, len(cacheMagic)+len(header)+1+len(data))
	record = append(record, cacheMagic...)
	record = append(record, header...)
	record = append(record, '\n')
	record = append(record, data...)
	d := &l.disk
	d.mu.Lock()
	defer d.mu.Unlock()
	size := int64(len(record))
	if d.cap >= 0 {
		if size > d.cap {
			return
		}
		if d.entries == nil || d.sinceScan >= max(int64(1), d.cap/20) {
			l.scanCache()
		}
		if d.total-d.entries[path].size+size > d.cap {
			l.scanCache()
			entries := make([]diskEntry, 0, len(d.entries))
			for _, e := range d.entries {
				if e.path != path {
					entries = append(entries, e)
				}
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].used.Before(entries[j].used) })
			target := max(size, d.cap-d.cap/10)
			for _, e := range entries {
				if d.total-d.entries[path].size+size <= target {
					break
				}
				if err := os.Remove(e.path); err == nil || os.IsNotExist(err) {
					d.total -= e.size
					delete(d.entries, e.path)
				}
			}
		}
	}
	if writeFileAtomic(path, record) != nil {
		return
	}
	if d.cap >= 0 {
		d.total += size - d.entries[path].size
		d.entries[path] = diskEntry{path, size, time.Now()}
		d.sinceScan += size
	}
}
