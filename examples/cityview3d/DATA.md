# Bundled neighborhood data

`../internal/cityview/neighborhood.json` is a compact extract of OpenStreetMap data near Flatiron,
Manhattan, downloaded on 2026-10-18. Geographic query bounds:

- South: 40.7400; west: -73.9910
- North: 40.7430; east: -73.9870

Source request:

https://api.openstreetmap.org/api/0.6/map.json?bbox=-73.9910,40.7400,-73.9870,40.7430

The extract retains building/building-part and highway ways, their node
coordinates, building relations, and source tags. Unrelated objects and edit
metadata were removed. Ways touching the query may extend beyond its rectangle.
Road segments are clipped to the rectangle when the scene is built.

Data © OpenStreetMap contributors, available under the Open Database License
(ODbL): https://www.openstreetmap.org/copyright

This is geographic source data, distinct from the repository's software license.
The viewer displays its attribution. Geometry is an approximation from available
tags, with no terrain or photographic facade textures. A default height is not a
measurement. The current parser omits courtyard relations and reports the count.
