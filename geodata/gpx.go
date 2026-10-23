package geodata

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"nimbleterminal.dev/ntmaps"
)

// ReadGPX reads GPX 1.0 or 1.1: waypoints as places, and routes and tracks
// as lines, one per track segment. Elevation, times and extensions are
// ignored.
func ReadGPX(r io.Reader, l Limits) (Data, error) {
	l = l.orDefault()
	raw, err := readAll(r, l)
	if err != nil {
		return Data{}, err
	}
	b := &builder{l: l, data: Data{Format: "gpx"}}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var (
		stack []string // local names of the open elements
		cur   *Feature // the waypoint, route or track being read
		seg   []ntmaps.Point
		root  bool
	)
	parent := func() string {
		if len(stack) < 2 {
			return ""
		}
		return stack[len(stack)-2]
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Data{}, fmt.Errorf("geodata: GPX: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			if len(stack) > maxDepth {
				return Data{}, fmt.Errorf("geodata: GPX: elements nested over %d deep: %w", maxDepth, ErrLimit)
			}
			if len(stack) == 1 {
				if t.Name.Local != "gpx" {
					return Data{}, fmt.Errorf("geodata: GPX: the root is <%s>, not <gpx>", t.Name.Local)
				}
				root = true
				continue
			}
			switch {
			case t.Name.Local == "wpt" && parent() == "gpx":
				p, err := b.attrPoint(t)
				if err != nil {
					return Data{}, err
				}
				cur = &Feature{Places: []ntmaps.Point{p}}
			case (t.Name.Local == "rte" || t.Name.Local == "trk") && parent() == "gpx":
				cur = &Feature{}
			case t.Name.Local == "trkseg" && parent() == "trk":
				seg = nil
			case (t.Name.Local == "rtept" && parent() == "rte") || (t.Name.Local == "trkpt" && parent() == "trkseg"):
				p, err := b.attrPoint(t)
				if err != nil {
					return Data{}, err
				}
				seg = append(seg, p)
			case t.Name.Local == "name" && cur != nil && (parent() == "wpt" || parent() == "rte" || parent() == "trk"):
				var s string
				if err := dec.DecodeElement(&s, &t); err != nil {
					return Data{}, fmt.Errorf("geodata: GPX: %w", err)
				}
				cur.Name = strings.TrimSpace(s)
				stack = stack[:len(stack)-1] // DecodeElement consumed the end.
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return Data{}, errors.New("geodata: GPX: unbalanced elements")
			}
			name := t.Name.Local
			from := parent()
			stack = stack[:len(stack)-1]
			switch {
			case name == "trkseg" && from == "trk" && cur != nil:
				if len(seg) > 0 {
					cur.Lines = append(cur.Lines, seg)
				}
				seg = nil
			case name == "rte" && from == "gpx" && cur != nil:
				if len(seg) > 0 {
					cur.Lines = append(cur.Lines, seg)
				}
				seg = nil
				fallthrough
			case (name == "wpt" || name == "trk") && from == "gpx" && cur != nil:
				if err := b.feature(*cur); err != nil {
					return Data{}, err
				}
				cur = nil
			}
		}
	}
	if !root {
		return Data{}, errors.New("geodata: GPX: no <gpx> element")
	}
	return b.data, nil
}

// attrPoint is the position in an element's lat and lon attributes.
func (b *builder) attrPoint(t xml.StartElement) (ntmaps.Point, error) {
	var lat, lon string
	for _, a := range t.Attr {
		switch a.Name.Local {
		case "lat":
			lat = a.Value
		case "lon":
			lon = a.Value
		}
	}
	la, err1 := strconv.ParseFloat(strings.TrimSpace(lat), 64)
	lo, err2 := strconv.ParseFloat(strings.TrimSpace(lon), 64)
	if err1 != nil || err2 != nil {
		return ntmaps.Point{}, fmt.Errorf("geodata: GPX: <%s> needs lat and lon numbers", t.Name.Local)
	}
	return b.point(la, lo)
}
