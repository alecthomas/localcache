package localcache

import "time"

type clocker interface {
	Now() time.Time
	Since(time.Time) time.Duration
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

type fakeClock struct {
	currentTime time.Time
}

func (f *fakeClock) Now() time.Time {
	f.advance(time.Second)
	return f.currentTime // every time we call now the clock advances a little.
}

func (f *fakeClock) Since(t time.Time) time.Duration {
	return f.currentTime.Sub(t)
}

func (f *fakeClock) advance(d time.Duration) {
	f.currentTime = f.currentTime.Add(d)
}
