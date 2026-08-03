package cronparser

import (
	"math"
	"math/rand"
)

// prng mirrors upstream's PRNG type: a function returning a float in [0,1).
type prng func() float64

// xfnv1a hashes a string to a 32-bit seed.
//
// Upstream:
//
//	let h = 2166136261 >>> 0;
//	h ^= str.charCodeAt(i);
//	h = Math.imul(h, 16777619);
//
// Two JS details matter:
//
//  1. charCodeAt returns a UTF-16 code unit, not a Unicode code point. Iterating
//     a Go string with `range` yields runes, which differ for any character
//     outside the BMP. Iterate UTF-16 code units to match.
//  2. Math.imul is a 32-bit signed multiply that wraps. uint32 multiplication in
//     Go wraps identically, so the bit pattern agrees.
func xfnv1a(s string) uint32 {
	h := uint32(2166136261)
	for _, u := range utf16Units(s) {
		h ^= uint32(u)
		h *= 16777619 // wraps like Math.imul
	}
	return h
}

// utf16Units returns the UTF-16 code units of s, matching JS string indexing.
func utf16Units(s string) []uint16 {
	var out []uint16
	for _, r := range s {
		if r <= 0xFFFF {
			out = append(out, uint16(r))
			continue
		}
		r -= 0x10000
		out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
	}
	return out
}

// mulberry32 is upstream's PRNG, transcribed.
//
// The subtle part is `let t = (seed += 0x6d2b79f5)`. In JS `seed` is a float64
// that is NEVER truncated to 32 bits, so it keeps growing past 2^32 across
// calls. Only the expressions that apply `|`, `^`, `>>>` or Math.imul coerce to
// 32 bits (via ToInt32/ToUint32, which take the value modulo 2^32).
//
// Modelling `seed` as uint32 would be wrong in general: ToUint32 of a float
// larger than 2^32 wraps, which happens to agree with uint32 addition — but
// only because the increment is added before every use. Keep it as float64 so
// the accumulation is literally what JS does, and coerce explicitly at each use.
func mulberry32(seed float64) prng {
	return func() float64 {
		seed += 0x6d2b79f5
		t := toUint32(seed)

		// t = Math.imul(t ^ (t >>> 15), t | 1)
		t = t ^ (t >> 15)
		t = t * (toUint32(seed) | 1)

		// t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
		inner := (t ^ (t >> 7)) * (t | 61)
		t = t ^ (t + inner)

		// return ((t ^ (t >>> 14)) >>> 0) / 4294967296
		return float64(t^(t>>14)) / 4294967296
	}
}

// toUint32 implements ECMAScript ToUint32: truncate toward zero, then take the
// result modulo 2^32.
func toUint32(f float64) uint32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	t := math.Trunc(f)
	m := math.Mod(t, 4294967296)
	if m < 0 {
		m += 4294967296
	}
	return uint32(m)
}

// seededRandom returns a PRNG seeded from str, or randomly when str is empty.
//
// Upstream: seededRandom.
func seededRandom(str string) prng {
	if str != "" {
		return mulberry32(float64(xfnv1a(str)))
	}
	return mulberry32(math.Floor(rand.Float64() * 10_000_000_000))
}
