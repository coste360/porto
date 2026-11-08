package optimizer

// rng is the optimizer's own PRNG (m9-plan §6.3): xoshiro256** seeded by splitmix64. It never touches math/rand,
// so a run is a function of (Input, Seed) only.
type rng struct{ s [4]uint64 }

func newRNG(seed uint64) *rng {
	r := &rng{}
	x := seed
	for i := range r.s {
		x += 0x9e3779b97f4a7c15
		z := x
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		r.s[i] = z ^ (z >> 31)
	}
	return r
}

func rotl(x uint64, k uint) uint64 { return (x << k) | (x >> (64 - k)) }

// next returns the next 64 random bits (xoshiro256**).
func (r *rng) next() uint64 {
	s := &r.s
	out := rotl(s[1]*5, 7) * 9
	t := s[1] << 17
	s[2] ^= s[0]
	s[3] ^= s[1]
	s[1] ^= s[2]
	s[0] ^= s[3]
	s[2] ^= t
	s[3] = rotl(s[3], 45)
	return out
}

// intn returns an integer in [0, n) (n > 0); the modulo bias is negligible for the small n used here.
func (r *rng) intn(n int) int {
	return int(r.next() % uint64(n)) //nolint:gosec // n is a small positive int
}
