// Package cpyrand is a byte-identical port of CPython's random module (the
// Mersenne Twister MT19937 generator plus the random.Random seeding and draw
// algorithms). It exists so the Go engine's RNG-driven choices reproduce
// Python's random.Random stream EXACTLY — the fix for ORACLE-SMOKE.md Finding 1,
// where Go's math/rand and CPython's Mersenne Twister diverged after the first
// RNG-keyed draw and cascaded into divergent generated text.
//
// The core (mt19937) is a direct transcription of CPython Modules/_randommodule.c
// (init_genrand / init_by_array / genrand_uint32 with the standard tempering).
// The random.Random surface (random / getrandbits / _randbelow / shuffle and the
// randrange/randint/choice helpers) lives in random.go.
package cpyrand

// MT19937 constants (the canonical 32-bit Mersenne Twister parameters).
const (
	n         = 624
	m         = 397
	matrixA   = 0x9908b0df // constant vector a
	upperMask = 0x80000000 // most significant w-r bits
	lowerMask = 0x7fffffff // least significant r bits
)

// mt19937 is the MT19937 state: the 624-word state vector plus the read index.
// When index == n, the state is exhausted and the next genrandUint32 regenerates
// the whole block (matching CPython's mti == N+1 / generate-block logic).
type mt19937 struct {
	state [n]uint32
	index int // mti in CPython terms; n (or n+1) means "needs regeneration"
}

// initGenrand seeds the state from a single 32-bit value. Direct port of
// CPython init_genrand (Knuth's MMIX-style LCG fill, 1812433253 multiplier).
func (mt *mt19937) initGenrand(s uint32) {
	mt.state[0] = s
	for i := 1; i < n; i++ {
		// state[i] = 1812433253 * (state[i-1] ^ (state[i-1] >> 30)) + i
		prev := mt.state[i-1]
		mt.state[i] = 1812433253*(prev^(prev>>30)) + uint32(i)
	}
	mt.index = n // force a regeneration on the first draw
}

// initByArray seeds the state from a key array. Direct port of CPython
// init_by_array — this is what random.Random(int) ultimately calls (the int seed
// is split into little-endian uint32 words and passed here).
func (mt *mt19937) initByArray(key []uint32) {
	mt.initGenrand(19650218)
	i, j := 1, 0
	keyLength := len(key)

	// k counts down max(N, key_length) iterations.
	k := n
	if keyLength > k {
		k = keyLength
	}
	for ; k > 0; k-- {
		prev := mt.state[i-1]
		// state[i] = (state[i] ^ ((prev ^ (prev>>30)) * 1664525)) + key[j] + j
		mt.state[i] = (mt.state[i] ^ ((prev ^ (prev >> 30)) * 1664525)) + key[j] + uint32(j)
		i++
		j++
		if i >= n {
			mt.state[0] = mt.state[n-1]
			i = 1
		}
		if j >= keyLength {
			j = 0
		}
	}
	for k = n - 1; k > 0; k-- {
		prev := mt.state[i-1]
		// state[i] = (state[i] ^ ((prev ^ (prev>>30)) * 1566083941)) - i
		mt.state[i] = (mt.state[i] ^ ((prev ^ (prev >> 30)) * 1566083941)) - uint32(i)
		i++
		if i >= n {
			mt.state[0] = mt.state[n-1]
			i = 1
		}
	}
	mt.state[0] = 0x80000000 // MSB is 1; assuring non-zero initial array
}

// genrandUint32 returns the next 32-bit output. Direct port of CPython
// genrand_uint32: regenerate the block when exhausted, then temper one word.
func (mt *mt19937) genrandUint32() uint32 {
	if mt.index >= n {
		// Generate N words at one time.
		var y uint32
		mag01 := [2]uint32{0x0, matrixA} // mag01[x] = x * MATRIX_A for x in {0,1}

		var kk int
		for kk = 0; kk < n-m; kk++ {
			y = (mt.state[kk] & upperMask) | (mt.state[kk+1] & lowerMask)
			mt.state[kk] = mt.state[kk+m] ^ (y >> 1) ^ mag01[y&0x1]
		}
		for ; kk < n-1; kk++ {
			y = (mt.state[kk] & upperMask) | (mt.state[kk+1] & lowerMask)
			mt.state[kk] = mt.state[kk+(m-n)] ^ (y >> 1) ^ mag01[y&0x1]
		}
		y = (mt.state[n-1] & upperMask) | (mt.state[0] & lowerMask)
		mt.state[n-1] = mt.state[m-1] ^ (y >> 1) ^ mag01[y&0x1]

		mt.index = 0
	}

	y := mt.state[mt.index]
	mt.index++

	// Tempering.
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}
