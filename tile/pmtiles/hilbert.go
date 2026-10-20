package pmtiles

// ZxyToID converts tile coordinates to a PMTiles tile ID: all tiles of
// lower zooms come first, and within a zoom tiles follow the Hilbert
// curve, so nearby tiles have nearby IDs.
func ZxyToID(z uint8, x, y uint32) uint64 {
	var acc uint64
	for t := uint8(0); t < z; t++ {
		acc += uint64(1) << (2 * t)
	}
	n := uint64(1) << z
	tx, ty := uint64(x), uint64(y)
	var d uint64
	for s := n / 2; s > 0; s /= 2 {
		var rx, ry uint64
		if tx&s > 0 {
			rx = 1
		}
		if ty&s > 0 {
			ry = 1
		}
		d += s * s * ((3 * rx) ^ ry)
		rotate(n, &tx, &ty, rx, ry)
	}
	return acc + d
}

// IDToZxy is the inverse of ZxyToID.
func IDToZxy(id uint64) (z uint8, x, y uint32) {
	var acc uint64
	for z = 0; z < 32; z++ {
		count := uint64(1) << (2 * z)
		if id < acc+count {
			break
		}
		acc += count
	}
	n := uint64(1) << z
	t := id - acc
	var tx, ty uint64
	for s := uint64(1); s < n; s *= 2 {
		rx := 1 & (t / 2)
		ry := 1 & (t ^ rx)
		rotate(s, &tx, &ty, rx, ry)
		tx += s * rx
		ty += s * ry
		t /= 4
	}
	return z, uint32(tx), uint32(ty)
}

func rotate(n uint64, x, y *uint64, rx, ry uint64) {
	if ry == 0 {
		if rx == 1 {
			*x = n - 1 - *x
			*y = n - 1 - *y
		}
		*x, *y = *y, *x
	}
}
