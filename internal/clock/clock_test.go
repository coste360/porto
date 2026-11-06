package clock

import (
	"sync"
	"testing"
	"time"
)

func TestSystem_NowIsUTC(t *testing.T) {
	if loc := (System{}).Now().Location(); loc != time.UTC {
		t.Fatalf("location = %v, want UTC", loc)
	}
}

func TestFake_NewFakeConvertsToUTC(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("tzdata not available")
	}
	in := time.Date(2026, 7, 1, 12, 0, 0, 0, rome)
	f := NewFake(in)
	if got := f.Now(); got.Location() != time.UTC || !got.Equal(in) {
		t.Fatalf("Now() = %v, want %v in UTC", got, in)
	}
}

func TestFake_Advance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		steps []time.Duration
		want  time.Time
	}{
		{"none", nil, start},
		{"one hour", []time.Duration{time.Hour}, start.Add(time.Hour)},
		{"back and forth", []time.Duration{time.Hour, -30 * time.Minute}, start.Add(30 * time.Minute)},
		{"across days", []time.Duration{48 * time.Hour}, start.AddDate(0, 0, 2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewFake(start)
			for _, d := range tt.steps {
				f.Advance(d)
			}
			if got := f.Now(); !got.Equal(tt.want) {
				t.Fatalf("Now() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("concurrent", func(t *testing.T) {
		f := NewFake(start)
		const workers, perWorker = 16, 100
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range perWorker {
					f.Advance(time.Second)
					_ = f.Now()
				}
			}()
		}
		wg.Wait()
		if got, want := f.Now(), start.Add(workers*perWorker*time.Second); !got.Equal(want) {
			t.Fatalf("Now() = %v, want %v", got, want)
		}
	})
}

func TestFake_Set(t *testing.T) {
	f := NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	want := time.Date(2030, 5, 6, 7, 8, 9, 0, time.UTC)
	f.Set(want)
	if got := f.Now(); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
}
