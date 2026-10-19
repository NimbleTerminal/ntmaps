package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/list"
	zone "github.com/lrstanley/bubblezone/v2"
)

type placeDelegate struct {
	list.DefaultDelegate
	zones  *zone.Manager
	prefix string
}

func (d placeDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	var rendered strings.Builder
	d.DefaultDelegate.Render(&rendered, m, index, item)
	fmt.Fprint(w, d.zones.Mark(d.prefix+strconv.Itoa(index), rendered.String()))
}
func (m *model) itemZone(index int) string { return m.itemPrefix + strconv.Itoa(index) }
