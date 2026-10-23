// Package geocode turns a place name into coordinates. Geocoder is the
// interface ntmaps uses for SetLocation; Nominatim is the OpenStreetMap
// implementation, with the identifying User-Agent and one-request-per-
// second pacing that its usage policy requires.
package geocode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"nimbleterminal.dev/ntmaps/internal/httpua"
)

// ErrNotFound is returned when a query matches nothing.
var ErrNotFound = errors.New("geocode: location not found")

// Result is the best match for a query.
type Result struct {
	Lat, Lng float64

	// Name is the service's display name for the match, when it has one.
	Name string
}

// Geocoder resolves a free-text query to coordinates.
type Geocoder interface {
	Geocode(ctx context.Context, query string) (Result, error)
}

// Func adapts a function to the Geocoder interface.
type Func func(ctx context.Context, query string) (Result, error)

// Geocode implements Geocoder.
func (f Func) Geocode(ctx context.Context, query string) (Result, error) { return f(ctx, query) }

const (
	defaultEndpoint = "https://nominatim.openstreetmap.org/search"
	defaultInterval = time.Second
	defaultTimeout  = 15 * time.Second
	maxBodyBytes    = 4 << 20
)

// Nominatim geocodes through a Nominatim server, by default the public
// OpenStreetMap instance. Its usage policy
// (https://operations.osmfoundation.org/policies/nominatim/) requires an
// identifying User-Agent and at most one request per second, which one
// Nominatim value enforces across its callers. The public service limit
// applies to all users of an application combined; per-instance pacing alone
// does not enforce that aggregate limit. Share one value within a process. Use a pointer so the pacing state is shared.
type Nominatim struct {
	// Endpoint is the search URL. Empty means the public OSM instance.
	Endpoint string

	// UserAgent identifies the application. Empty means the ntmaps
	// default. Public OSM services require an app-specific identity. Not sent under GOOS=js, where the browser sets its own.
	UserAgent string

	// Client performs the requests. nil uses a client with a 15s timeout.
	Client *http.Client

	// MinInterval is the pacing between requests. Zero means one second;
	// a private server can set it lower.
	MinInterval time.Duration

	mu   sync.Mutex
	next time.Time
}

var defaultClient = &http.Client{Timeout: defaultTimeout}

type nominatimHit struct {
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
}

// Geocode implements Geocoder. It waits for the pacing window, which
// ctx can cut short.
func (n *Nominatim) Geocode(ctx context.Context, query string) (Result, error) {
	if err := n.pace(ctx); err != nil {
		return Result{}, err
	}
	endpoint := n.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return Result{}, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("limit", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, err
	}
	httpua.Set(req, n.UserAgent)
	req.Header.Set("Accept", "application/json")
	client := n.Client
	if client == nil {
		client = defaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("geocode: %s: %s", resp.Status, string(body))
	}
	var hits []nominatimHit
	if err := json.Unmarshal(body, &hits); err != nil {
		return Result{}, fmt.Errorf("geocode: decode response: %w", err)
	}
	if len(hits) == 0 {
		return Result{}, ErrNotFound
	}
	lat, err := strconv.ParseFloat(hits[0].Lat, 64)
	if err != nil {
		return Result{}, fmt.Errorf("geocode: latitude %q: %w", hits[0].Lat, err)
	}
	lng, err := strconv.ParseFloat(hits[0].Lon, 64)
	if err != nil {
		return Result{}, fmt.Errorf("geocode: longitude %q: %w", hits[0].Lon, err)
	}
	return Result{Lat: lat, Lng: lng, Name: hits[0].DisplayName}, nil
}

// pace blocks until this value's next request slot, then claims the one
// after it.
func (n *Nominatim) pace(ctx context.Context) error {
	interval := n.MinInterval
	if interval <= 0 {
		interval = defaultInterval
	}
	n.mu.Lock()
	now := time.Now()
	slot := n.next
	if slot.Before(now) {
		slot = now
	}
	n.next = slot.Add(interval)
	n.mu.Unlock()
	if wait := time.Until(slot); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
