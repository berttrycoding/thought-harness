package cpyrand

import "math/big"

// Random is a byte-identical port of CPython's random.Random. It wraps the
// MT19937 core and reproduces random.Random's seeding (random_seed) and draw
// algorithms (random, getrandbits, _randbelow_with_getrandbits, shuffle).
//
// The PUBLIC method surface mirrors the subset of *math/rand.Rand the engine
// actually calls — Intn, Float64, Shuffle — so call sites can swap to this type
// with the SAME draw semantics as Python (Intn == randrange/_randbelow,
// Float64 == random(), Shuffle == random.Random.shuffle). The lowercase
// random/getrandbits/randbelow helpers are the exact-named Python primitives,
// exported via the helper methods below where a call site needs them.
type Random struct {
	mt mt19937
}

// New constructs a Random seeded exactly as CPython's random.Random(seed) — the
// integer seed is reduced to its absolute value, split into little-endian 32-bit
// words (the empty key becomes [0]), and fed to init_by_array. This matches
// CPython random_seed for an integer argument.
//
// The seed is a uint64 to match the engine (Config.Seed is unsigned and small);
// CPython takes abs() of arbitrary-precision ints, but every engine seed is a
// small non-negative value, so a uint64 covers the real range with identical
// words. For seed 7 the key is [7]; for seed 0 the key is [0].
func New(seed uint64) *Random {
	r := &Random{}
	r.Seed(seed)
	return r
}

// Seed re-seeds the generator in place, identically to New.
func (r *Random) Seed(seed uint64) {
	r.mt.initByArray(seedToKey(seed))
}

// seedToKey splits a non-negative integer seed into the little-endian uint32 key
// CPython's random_seed builds via _PyLong_AsByteArray + chunking. A zero seed
// yields a single zero word (CPython's loop runs at least once: keymax starts at
// 1 for n == 0).
func seedToKey(seed uint64) []uint32 {
	if seed == 0 {
		return []uint32{0}
	}
	var key []uint32
	for seed > 0 {
		key = append(key, uint32(seed&0xffffffff))
		seed >>= 32
	}
	return key
}

// random returns the next float64 in [0, 1) exactly as CPython random_random:
// 27 high bits from one draw, 26 high bits from the next, combined into a 53-bit
// mantissa scaled by 2**-53.
func (r *Random) random() float64 {
	a := r.mt.genrandUint32() >> 5 // 27 bits
	b := r.mt.genrandUint32() >> 6 // 26 bits
	return (float64(a)*67108864.0 + float64(b)) * (1.0 / 9007199254740992.0)
}

// getrandbits returns a k-bit non-negative integer exactly as CPython
// random_getrandbits. For k <= 32 it is one draw shifted down; for k > 32 it
// assembles little-endian 32-bit words with the final word masked to the
// remaining bits. Returned as *big.Int so k can exceed 64 (Python ints are
// unbounded). Callers needing small k use GetRandBitsUint64 / the helpers.
func (r *Random) getrandbits(k int) *big.Int {
	if k <= 0 {
		return big.NewInt(0)
	}
	if k <= 32 {
		v := r.mt.genrandUint32() >> (32 - uint(k))
		return new(big.Int).SetUint64(uint64(v))
	}

	// Assemble little-endian 32-bit words; the final (most significant) word is
	// masked to the remaining bits. CPython builds an array of words then
	// _PyLong_FromByteArray little-endian.
	result := new(big.Int)
	shift := uint(0)
	remaining := k
	tmp := new(big.Int)
	for remaining > 0 {
		take := remaining
		if take > 32 {
			take = 32
		}
		word := r.mt.genrandUint32()
		if take < 32 {
			word >>= 32 - uint(take)
		}
		tmp.SetUint64(uint64(word))
		tmp.Lsh(tmp, shift)
		result.Or(result, tmp)
		shift += 32
		remaining -= 32
	}
	return result
}

// getrandbitsU32 is the fast path for k <= 32, returning a plain uint32.
func (r *Random) getrandbitsU32(k int) uint32 {
	if k <= 0 {
		return 0
	}
	return r.mt.genrandUint32() >> (32 - uint(k))
}

// randbelow is _randbelow_with_getrandbits(n): the rejection-sampling unbiased
// integer in [0, n). n == 0 returns 0 (matches CPython). For n that fits in
// 32 bits it uses the uint32 fast path; otherwise the big.Int path. The draw
// SEQUENCE is identical either way (one getrandbits(k) per loop iteration).
func (r *Random) randbelow(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	k := bitLen(n)
	if k <= 32 {
		v := uint64(r.getrandbitsU32(k))
		for v >= n {
			v = uint64(r.getrandbitsU32(k))
		}
		return v
	}
	nb := new(big.Int).SetUint64(n)
	v := r.getrandbits(k)
	for v.Cmp(nb) >= 0 {
		v = r.getrandbits(k)
	}
	return v.Uint64()
}

// bitLen is Python's int.bit_length for a non-negative value.
func bitLen(n uint64) int {
	k := 0
	for n > 0 {
		k++
		n >>= 1
	}
	return k
}

// ---- Public surface mirroring the *math/rand.Rand subset the engine uses ----

// Float64 returns a float64 in [0, 1) == Python random.Random.random().
func (r *Random) Float64() float64 { return r.random() }

// Intn returns a non-negative int in [0, n) == Python randrange(n) == _randbelow(n).
// Mirrors *math/rand.Rand.Intn (which panics for n <= 0); we follow CPython
// randrange semantics where the underlying _randbelow(0) == 0, but a non-positive
// argument is a caller bug, so we panic like math/rand to surface it.
func (r *Random) Intn(n int) int {
	if n <= 0 {
		panic("cpyrand: Intn argument must be positive")
	}
	return int(r.randbelow(uint64(n)))
}

// Float64n is not part of math/rand; kept out deliberately.

// Shuffle permutes n elements using swap, identically to Python
// random.Random.shuffle: for i := n-1; i > 0; i-- { j = _randbelow(i+1); swap(i, j) }.
// NOTE: this is NOT the same draw pattern as math/rand.Shuffle (which uses a
// different bound and a Fisher-Yates variant); using CPython's order is the whole
// point — it makes shuffle results byte-identical to Python.
func (r *Random) Shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		j := int(r.randbelow(uint64(i + 1)))
		swap(i, j)
	}
}

// ---- Exact-named Python primitives / helpers for call sites that want them ----

// Random is Python random.Random.random() under its Python name.
func (r *Random) Random() float64 { return r.random() }

// GetRandBits returns a k-bit integer as *big.Int (Python getrandbits(k)).
func (r *Random) GetRandBits(k int) *big.Int { return r.getrandbits(k) }

// GetRandBitsUint64 returns getrandbits(k) for k <= 64 as a uint64 (convenience).
func (r *Random) GetRandBitsUint64(k int) uint64 {
	if k <= 32 {
		return uint64(r.getrandbitsU32(k))
	}
	return r.getrandbits(k).Uint64()
}

// RandBelow returns _randbelow_with_getrandbits(n).
func (r *Random) RandBelow(n uint64) uint64 { return r.randbelow(n) }

// Randrange returns Python randrange(stop) for the single-argument form (a
// non-negative integer in [0, stop)). Equivalent to Intn.
func (r *Random) Randrange(stop int) int { return r.Intn(stop) }

// RandrangeStartStop returns Python randrange(start, stop) == start + _randbelow(stop-start).
func (r *Random) RandrangeStartStop(start, stop int) int {
	width := stop - start
	if width <= 0 {
		panic("cpyrand: empty range for RandrangeStartStop")
	}
	return start + int(r.randbelow(uint64(width)))
}

// Randint returns Python randint(a, b) == randrange(a, b+1), inclusive on both ends.
func (r *Random) Randint(a, b int) int {
	return a + int(r.randbelow(uint64(b-a+1)))
}

// Choice returns a uniformly chosen element index in [0, n) == Python
// choice(seq) via _randbelow(len(seq)). Returns the index so it is type-agnostic.
func (r *Random) Choice(n int) int {
	if n <= 0 {
		panic("cpyrand: Choice from empty sequence")
	}
	return int(r.randbelow(uint64(n)))
}
