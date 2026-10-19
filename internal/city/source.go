package city

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const MaxSourceBytes = 16 << 20

type osmElement struct {
	Type     string `json:"type"`
	ID       int64  `json:"id"`
	Lat, Lon float64
	Nodes    []int64           `json:"nodes"`
	Geometry []Coordinate      `json:"geometry"`
	Tags     map[string]string `json:"tags"`
	Members  []struct {
		Type string `json:"type"`
		Ref  int64  `json:"ref"`
	} `json:"members"`
}

// Decode accepts OSM Map API JSON (nodes + way references) or Overpass JSON
// with full, unclipped way geometry. Building relations are counted and omitted:
// this first implementation does not fill courtyard holes or join member ways.
func Decode(r io.Reader, bounds Bounds) (Dataset, error) {
	d := Dataset{Bounds: bounds, Attribution: "© OpenStreetMap contributors (ODbL)"}
	if err := bounds.Validate(); err != nil {
		return d, err
	}
	raw, err := io.ReadAll(io.LimitReader(r, MaxSourceBytes+1))
	if err != nil {
		return d, err
	}
	if len(raw) > MaxSourceBytes {
		return d, fmt.Errorf("city: source exceeds %d bytes", MaxSourceBytes)
	}
	var doc struct {
		Elements []osmElement `json:"elements"`
		Remark   string       `json:"remark"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		return d, fmt.Errorf("city: decode OSM JSON: %w", err)
	}
	if doc.Remark != "" {
		return d, fmt.Errorf("city: incomplete source: %s", doc.Remark)
	}
	nodes := make(map[int64]Coordinate)
	excluded := make(map[int64]bool)
	for _, e := range doc.Elements {
		if e.Type == "node" {
			nodes[e.ID] = Coordinate{e.Lat, e.Lon}
		}
		if e.Type == "relation" && (buildingTag(e.Tags["building"]) || buildingTag(e.Tags["building:part"])) {
			d.SkippedRelations++
			for _, m := range e.Members {
				if m.Type == "way" {
					excluded[m.Ref] = true
				}
			}
		}
	}
	for _, e := range doc.Elements {
		if e.Type != "way" {
			continue
		}
		part := buildingTag(e.Tags["building:part"])
		building := buildingTag(e.Tags["building"]) || part
		road := e.Tags["highway"] != ""
		if !building && !road {
			continue
		}
		points := e.Geometry
		if len(points) == 0 {
			points = make([]Coordinate, 0, len(e.Nodes))
			for _, id := range e.Nodes {
				p, ok := nodes[id]
				if !ok {
					points = nil
					break
				}
				points = append(points, p)
			}
		}
		valid := len(points) >= 2 && len(points) <= 512
		for _, p := range points {
			if !p.Valid() {
				valid = false
			}
		}
		if building {
			// Require topological closure when node references are supplied.
			closed := len(points) > 3 && points[0] == points[len(points)-1]
			if len(e.Nodes) > 0 {
				closed = closed && e.Nodes[0] == e.Nodes[len(e.Nodes)-1]
			}
			if !valid || !closed || excluded[e.ID] {
				d.SkippedWays++
				continue
			}
			if len(d.Buildings) >= 2000 {
				return d, fmt.Errorf("city: source exceeds 2000 building limit")
			}
			d.Buildings = append(d.Buildings, Building{"way/" + strconv.FormatInt(e.ID, 10), points, e.Tags, part})
		} else if road && valid && len(d.Roads) < 4000 {
			d.Roads = append(d.Roads, Road{points})
		}
	}
	return d, nil
}
func buildingTag(s string) bool { return s != "" && s != "no" }

// Client is an explicit, bounded one-shot neighborhood download. The viewer
// uses bundled/file data by default; there are no automatic requests on movement.
type Client struct {
	HTTP     *http.Client
	Endpoint string
}

func (c Client) Fetch(ctx context.Context, b Bounds) (Dataset, error) {
	if err := b.Validate(); err != nil {
		return Dataset{}, err
	}
	p := Projection{b.Center()}
	x, y := p.Project(Coordinate{b.North, b.East})
	if x > 750 || y > 750 {
		return Dataset{}, fmt.Errorf("city: live requests are limited to a 1.5 km neighborhood")
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = "https://api.openstreetmap.org/api/0.6/map.json"
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return Dataset{}, err
	}
	q := u.Query()
	q.Set("bbox", fmt.Sprintf("%.7f,%.7f,%.7f,%.7f", b.West, b.South, b.East, b.North))
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Dataset{}, err
	}
	req.Header.Set("User-Agent", "ntcharts-osm-city-prototype/0.1 (https://github.com/NimbleMarkets/ntcharts-osm)")
	req.Header.Set("Accept", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Dataset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Dataset{}, fmt.Errorf("city: map request: %s", resp.Status)
	}
	return Decode(resp.Body, b)
}
