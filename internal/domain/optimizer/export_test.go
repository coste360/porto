package optimizer

// ScoreOf scores a layout of the input (nil = unplaced) with the optimizer objective (tests only).
func ScoreOf(in Input, at []*Placement) (Score, error) {
	r, err := prepare(in, nil)
	if err != nil {
		return Score{}, err
	}
	st := make([]pos, len(r.items))
	for i, p := range at {
		if p == nil {
			continue
		}
		b, ok := r.w.boxOf(r.items[i].l, r.items[i].b, *p)
		if !ok {
			continue
		}
		st[i] = pos{set: true, b: b, keep: r.items[i].cur != nil && *r.items[i].src.Current == *p}
	}
	return r.score(st), nil
}
