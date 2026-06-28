package cpyrand

import (
	"math/big"
	"testing"
)

// Golden reference sequences captured from REAL CPython (python3 random module).
// Generation (reproduce verbatim):
//
//	python3 - <<'PY'
//	import random
//	for s in (7, 0, 42, 12345):
//	    r=random.Random(s); print(s, [r.getrandbits(32) for _ in range(8)])
//	    r=random.Random(s); print(s, [r.random() for _ in range(8)])
//	    r=random.Random(s); print(s, [r._randbelow(8) for _ in range(20)])
//	    r=random.Random(s); x=list(range(10)); r.shuffle(x); print(s, x)
//	PY
//
// Any single divergent draw MUST fail the test (exact equality, no tolerance on
// the integer draws; bit-exact on the float mantissa).

type golden struct {
	seed          uint64
	getrandbits32 [8]uint32
	random        [8]float64
	randbelow8    [20]int
	shuffle10     [10]int
	// getrandbits for k > 32 (bit-exact, big-int valued); decimal strings.
	getrandbits53  [4]string
	getrandbits64  [4]uint64
	getrandbits100 [4]string
	randrange100   [10]int
	randint16      [10]int
}

var goldens = []golden{
	{
		seed:           7,
		getrandbits32:  [8]uint32{1390851128, 4071050724, 647892279, 1695753998, 2795742288, 207388624, 311111475, 3527346212},
		random:         [8]float64{0.32383276483316237, 0.15084917392450192, 0.6509344730398537, 0.07243628666754276, 0.5358820043066892, 0.36568891691258554, 0.057998924774706806, 0.5074357331894203},
		randbelow8:     [20]int{5, 2, 6, 0, 1, 1, 5, 0, 3, 0, 1, 6, 6, 1, 3, 1, 6, 0, 1, 3},
		shuffle10:      [10]int{8, 3, 1, 4, 7, 0, 9, 6, 2, 5},
		getrandbits53:  [4]string{"8537610396283960", "3556250748849463", "434924069037136", "7397381398802227"},
		getrandbits64:  [4]uint64{17485029721327973432, 7283207964119141687, 890727360438182992, 15149836622520594227},
		getrandbits100: [4]string{"487320478161116480663150048312", "1035705106444046403401351930960", "742026323667635606410750824491", "277887688554896545928674083957"},
		randrange100:   [10]int{41, 19, 50, 83, 6, 9, 68, 12, 46, 74},
		randint16:      [10]int{3, 2, 4, 6, 1, 1, 5, 1, 3, 5},
	},
	{
		seed:           0,
		getrandbits32:  [8]uint32{3626764237, 1654615998, 3255389356, 3823568514, 1806341205, 173879092, 1112038970, 4146640122},
		random:         [8]float64{0.8444218515250481, 0.7579544029403025, 0.420571580830845, 0.25891675029296335, 0.5112747213686085, 0.4049341374504143, 0.7837985890347726, 0.30331272607892745},
		randbelow8:     [20]int{6, 6, 0, 4, 7, 6, 4, 7, 5, 3, 2, 4, 2, 1, 4, 2, 4, 1, 1, 5},
		shuffle10:      [10]int{7, 8, 1, 5, 3, 4, 2, 0, 9, 6},
		getrandbits53:  [4]string{"3469980719646669", "8018604117806252", "364648824738901", "8696133065399866"},
		getrandbits64:  [4]uint64{7106521602475165645, 16422101724900707500, 746805015404516437, 17809683713383489082},
		getrandbits100: [4]string{"1169245609517217401678349469645", "1208935935994293442776782246997", "1141276462758851372969594381922", "1212453347278431371654525814329"},
		randrange100:   [10]int{49, 97, 53, 5, 33, 65, 62, 51, 38, 61},
		randint16:      [10]int{4, 4, 1, 3, 5, 4, 4, 3, 4, 3},
	},
	{
		seed:           42,
		getrandbits32:  [8]uint32{2746317213, 478163327, 107420369, 3184935163, 1181241943, 1051802512, 958682846, 599310825},
		random:         [8]float64{0.6394267984578837, 0.025010755222666936, 0.27502931836911926, 0.22321073814882275, 0.7364712141640124, 0.6766994874229113, 0.8921795677048454, 0.08693883262941615},
		randbelow8:     [20]int{1, 0, 4, 3, 3, 2, 1, 1, 6, 0, 0, 1, 3, 3, 0, 3, 6, 3, 7, 4},
		shuffle10:      [10]int{7, 3, 2, 8, 5, 6, 9, 4, 0, 1},
		getrandbits53:  [4]string{"1002783120652701", "6679292727990993", "2205789010285143", "1256845828445918"},
		getrandbits64:  [4]uint64{2053695854357871005, 13679192365072849617, 4517457392071889495, 2574020394472462046},
		getrandbits100: [4]string{"873491343714207852616756591005", "176140902141063639299770569303", "925123444424254823561077285033", "719941466287131323558231662928"},
		randrange100:   [10]int{81, 14, 3, 94, 35, 31, 28, 17, 94, 13},
		randint16:      [10]int{6, 1, 1, 6, 3, 2, 2, 2, 6, 1},
	},
	{
		seed:           12345,
		getrandbits32:  [8]uint32{1789368711, 3146859322, 43676229, 3522623596, 3544234957, 3448207591, 1282648386, 3672791226},
		random:         [8]float64{0.41661987254534116, 0.010169169457068361, 0.8252065092537432, 0.2986398551995928, 0.3684116894884757, 0.19366134904507426, 0.5660081687288613, 0.1616878239293682},
		randbelow8:     [20]int{6, 0, 4, 5, 3, 4, 6, 2, 5, 1, 6, 4, 2, 2, 5, 1, 6, 2, 2, 3},
		shuffle10:      [10]int{8, 7, 3, 5, 1, 2, 9, 4, 0, 6},
		getrandbits53:  [4]string{"6599442377972103", "7387476936782405", "7231418505673677", "7702402357766466"},
		getrandbits64:  [4]uint64{13515657874892102023, 15129553140981592645, 14809938836708178893, 15774518201988393282},
		getrandbits100: [4]string{"1030771796917419777846831192455", "1053626799213344948965825625037", "332256083118531044858259323495", "408951041752918591462038694213"},
		randrange100:   [10]int{53, 93, 1, 38, 47, 24, 34, 72, 55, 20},
		randint16:      [10]int{4, 6, 1, 3, 3, 2, 3, 5, 4, 2},
	},
}

func TestGetRandBits32(t *testing.T) {
	for _, g := range goldens {
		r := New(g.seed)
		for i, want := range g.getrandbits32 {
			got := r.GetRandBitsUint64(32)
			if uint32(got) != want {
				t.Fatalf("seed=%d getrandbits(32)[%d]: got %d want %d", g.seed, i, got, want)
			}
		}
	}
}

func TestRandomFloat(t *testing.T) {
	for _, g := range goldens {
		r := New(g.seed)
		for i, want := range g.random {
			got := r.Float64()
			if got != want { // bit-exact; the mantissa is constructed identically
				t.Fatalf("seed=%d random()[%d]: got %.17g want %.17g", g.seed, i, got, want)
			}
		}
	}
}

func TestRandBelow8(t *testing.T) {
	for _, g := range goldens {
		r := New(g.seed)
		for i, want := range g.randbelow8 {
			got := r.Intn(8) // Intn == randrange(8) == _randbelow(8)
			if got != want {
				t.Fatalf("seed=%d _randbelow(8)[%d]: got %d want %d", g.seed, i, got, want)
			}
		}
	}
}

func TestRandBelowMatchesIntnAndRandrange(t *testing.T) {
	// _randbelow(8), Intn(8), Randrange(8) and RandBelow(8) must be the identical draw.
	for _, g := range goldens {
		a, b, c, d := New(g.seed), New(g.seed), New(g.seed), New(g.seed)
		for i := 0; i < 20; i++ {
			va := a.Intn(8)
			vb := b.Randrange(8)
			vc := int(c.RandBelow(8))
			vd := d.Choice(8)
			if va != vb || va != vc || va != vd {
				t.Fatalf("seed=%d draw %d: Intn=%d Randrange=%d RandBelow=%d Choice=%d disagree", g.seed, i, va, vb, vc, vd)
			}
		}
	}
}

func TestShuffle10(t *testing.T) {
	for _, g := range goldens {
		r := New(g.seed)
		x := make([]int, 10)
		for i := range x {
			x[i] = i
		}
		r.Shuffle(len(x), func(i, j int) { x[i], x[j] = x[j], x[i] })
		for i := range x {
			if x[i] != g.shuffle10[i] {
				t.Fatalf("seed=%d shuffle: got %v want %v", g.seed, x, g.shuffle10)
			}
		}
	}
}

func TestGetRandBitsK(t *testing.T) {
	for _, g := range goldens {
		// k=53 (used by random()-equivalents in Python's getrandbits path)
		r := New(g.seed)
		for i, want := range g.getrandbits53 {
			got := r.GetRandBits(53).String()
			if got != want {
				t.Fatalf("seed=%d getrandbits(53)[%d]: got %s want %s", g.seed, i, got, want)
			}
		}
		// k=64
		r = New(g.seed)
		for i, want := range g.getrandbits64 {
			got := r.GetRandBitsUint64(64)
			if got != want {
				t.Fatalf("seed=%d getrandbits(64)[%d]: got %d want %d", g.seed, i, got, want)
			}
		}
		// k=100 (> 64; exercises the multi-word big.Int assembly + final-word masking)
		r = New(g.seed)
		for i, wantStr := range g.getrandbits100 {
			got := r.GetRandBits(100)
			want, ok := new(big.Int).SetString(wantStr, 10)
			if !ok {
				t.Fatalf("bad golden %q", wantStr)
			}
			if got.Cmp(want) != 0 {
				t.Fatalf("seed=%d getrandbits(100)[%d]: got %s want %s", g.seed, i, got.String(), wantStr)
			}
		}
	}
}

func TestRandrange100(t *testing.T) {
	for _, g := range goldens {
		r := New(g.seed)
		for i, want := range g.randrange100 {
			got := r.Randrange(100)
			if got != want {
				t.Fatalf("seed=%d randrange(100)[%d]: got %d want %d", g.seed, i, got, want)
			}
		}
	}
}

func TestRandint16(t *testing.T) {
	for _, g := range goldens {
		r := New(g.seed)
		for i, want := range g.randint16 {
			got := r.Randint(1, 6)
			if got != want {
				t.Fatalf("seed=%d randint(1,6)[%d]: got %d want %d", g.seed, i, got, want)
			}
		}
	}
}

// TestStreamConsumptionOrder verifies a MIXED draw sequence stays byte-identical
// to Python — this is the real engine pattern (Float64 then Intn in continuous
// Wander). Golden from:
//
//	python3 -c "import random; r=random.Random(7); print([round(r.random(),17) if i%2==0 else r.randrange(4) for i in range(8)])"
func TestMixedStreamSeed7(t *testing.T) {
	// random()=draw0, randrange(4)=draw1, ... interleaved on one generator.
	wantFloats := []float64{0.32383276483316237, 0.3948234964231735, 0.07243628666754276, 0.36568891691258554}
	wantInts := []int{1, 0, 0, 0} // randrange(4) at the odd positions
	r := New(7)
	fi, ii := 0, 0
	for i := 0; i < 8; i++ {
		if i%2 == 0 {
			got := r.Float64()
			if got != wantFloats[fi] {
				t.Fatalf("mixed seed=7 random()[pos %d]: got %.17g want %.17g", i, got, wantFloats[fi])
			}
			fi++
		} else {
			got := r.Intn(4)
			if got != wantInts[ii] {
				t.Fatalf("mixed seed=7 randrange(4)[pos %d]: got %d want %d", i, got, wantInts[ii])
			}
			ii++
		}
	}
}

func TestRandBelowZero(t *testing.T) {
	r := New(7)
	if got := r.RandBelow(0); got != 0 {
		t.Fatalf("_randbelow(0): got %d want 0", got)
	}
}
