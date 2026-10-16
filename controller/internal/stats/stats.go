// Package stats turns live power readings into energy statistics: 5-minute
// buckets kept for 90 days and hourly buckets kept indefinitely, with day
// and month views computed in the site's time zone.
package stats

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/the-asad-iqbal/async-os/controller/internal/plant"
	"github.com/the-asad-iqbal/async-os/controller/internal/store"
)

// Energy series, in Wh.
const (
	GridImport = "grid_import"
	GridExport = "grid_export"
	PV         = "pv"
	Genset     = "genset"
	Load       = "load"
)

// Power series, in W. Grid is signed (positive when importing).
const (
	PowerGrid   = "grid"
	PowerPV     = "pv"
	PowerGenset = "genset"
	PowerLoad   = "load"
)

// Periods for queries.
const (
	Period5m    = "5m"
	PeriodHour  = "hour"
	PeriodDay   = "day"
	PeriodMonth = "month"
)

const (
	bucketSize    = 5 * time.Minute
	retention5m   = 90 * 24 * time.Hour
	maxSampleGap  = 10 * time.Second
	maxQueryRange = 400 * 24 * time.Hour
)

// PowerStats summarize power over a bucket.
type PowerStats struct {
	Mean float64 `json:"mean"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
}

// Bucket is energy and power over one period.
type Bucket struct {
	Start  time.Time             `json:"start"`
	Energy map[string]float64    `json:"energy_wh"`
	Power  map[string]PowerStats `json:"power,omitempty"`
	// Seconds of data in the bucket.
	Seconds float64 `json:"seconds"`
}

func newBucket(start time.Time) *Bucket {
	return &Bucket{Start: start, Energy: map[string]float64{}, Power: map[string]PowerStats{}}
}

// merge adds o into b.
func (b *Bucket) merge(o *Bucket) {
	for k, v := range o.Energy {
		b.Energy[k] += v
	}
	for k, p := range o.Power {
		cur, ok := b.Power[k]
		if !ok {
			b.Power[k] = p
			continue
		}
		total := b.Seconds + o.Seconds
		if total > 0 {
			cur.Mean = (cur.Mean*b.Seconds + p.Mean*o.Seconds) / total
		}
		cur.Min = math.Min(cur.Min, p.Min)
		cur.Max = math.Max(cur.Max, p.Max)
		b.Power[k] = cur
	}
	b.Seconds += o.Seconds
}

type sample struct {
	time   time.Time
	energy map[string]float64 // instantaneous power per energy series
	power  map[string]float64
}

// Recorder accumulates snapshots into buckets.
type Recorder struct {
	store *store.Store
	log   *slog.Logger

	mu      sync.Mutex
	last    *sample
	current *Bucket
	sums    map[string]float64 // power x seconds, for means
}

// NewRecorder returns a recorder writing to s.
func NewRecorder(s *store.Store, log *slog.Logger) *Recorder {
	return &Recorder{store: s, log: log, sums: map[string]float64{}}
}

func sampleOf(s plant.Snapshot) sample {
	out := sample{time: s.Time, energy: map[string]float64{}, power: map[string]float64{}}
	out.energy[PV] = math.Max(0, s.PVW)
	out.power[PowerPV] = s.PVW
	if s.GridW != nil {
		out.energy[GridImport] = math.Max(0, *s.GridW)
		out.energy[GridExport] = math.Max(0, -*s.GridW)
		out.power[PowerGrid] = *s.GridW
	}
	if s.GensetW != nil {
		out.energy[Genset] = math.Max(0, *s.GensetW)
		out.power[PowerGenset] = *s.GensetW
	}
	if s.LoadW != nil {
		out.energy[Load] = *s.LoadW
		out.power[PowerLoad] = *s.LoadW
	}
	return out
}

// Observe integrates one snapshot.
func (r *Recorder) Observe(s plant.Snapshot) {
	if s.Time.IsZero() {
		return
	}
	cur := sampleOf(s)

	r.mu.Lock()
	defer r.mu.Unlock()

	start := cur.time.Truncate(bucketSize)
	if r.current != nil && !r.current.Start.Equal(start) {
		r.flushLocked()
	}
	if r.current == nil {
		r.current = newBucket(start)
		r.sums = map[string]float64{}
	}

	if prev := r.last; prev != nil {
		dt := cur.time.Sub(prev.time)
		if dt > 0 && dt <= maxSampleGap {
			sec := dt.Seconds()
			// Trapezoid rule, only where both samples have the series.
			for k, p1 := range cur.energy {
				if p0, ok := prev.energy[k]; ok {
					r.current.Energy[k] += (p0 + p1) / 2 * sec / 3600
				}
			}
			for k, p := range cur.power {
				r.sums[k] += p * sec
				stats, ok := r.current.Power[k]
				if !ok {
					stats = PowerStats{Min: p, Max: p}
				}
				stats.Min, stats.Max = math.Min(stats.Min, p), math.Max(stats.Max, p)
				r.current.Power[k] = stats
			}
			r.current.Seconds += sec
			for k, stats := range r.current.Power {
				stats.Mean = r.sums[k] / r.current.Seconds
				r.current.Power[k] = stats
			}
		}
	}
	r.last = &cur
}

// Flush writes the bucket in progress, e.g. on shutdown.
func (r *Recorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil {
		r.flushLocked()
	}
}

func (r *Recorder) flushLocked() {
	b := r.current
	r.current = nil
	if b.Seconds == 0 {
		return
	}
	hour := b.Start.Truncate(time.Hour)
	err := r.store.Update(func(tx *bolt.Tx) error {
		// A restart inside a bucket must add to it, not replace it.
		existing := newBucket(b.Start)
		if err := store.GetJSON(tx, store.BucketStats5m, store.TimeKey(b.Start), existing); err == nil {
			existing.merge(b)
			b = existing
		}
		if err := store.PutJSON(tx, store.BucketStats5m, store.TimeKey(b.Start), b); err != nil {
			return err
		}
		if err := rollupHour(tx, hour); err != nil {
			return err
		}
		return prune5m(tx, b.Start.Add(-retention5m))
	})
	if err != nil {
		r.log.Error("statistics write failed", "error", err)
	}
}

// rollupHour recomputes an hourly bucket from its 5-minute buckets.
func rollupHour(tx *bolt.Tx, hour time.Time) error {
	agg := newBucket(hour)
	c := tx.Bucket(store.BucketStats5m).Cursor()
	end := store.TimeKey(hour.Add(time.Hour))
	for k, v := c.Seek(store.TimeKey(hour)); k != nil && string(k) < string(end); k, v = c.Next() {
		var b Bucket
		if err := json.Unmarshal(v, &b); err != nil {
			return err
		}
		agg.merge(&b)
	}
	return store.PutJSON(tx, store.BucketStats1h, store.TimeKey(hour), agg)
}

func prune5m(tx *bolt.Tx, before time.Time) error {
	c := tx.Bucket(store.BucketStats5m).Cursor()
	limit := store.TimeKey(before)
	for k, _ := c.First(); k != nil && string(k) < string(limit); k, _ = c.First() {
		if err := c.Delete(); err != nil {
			return err
		}
	}
	return nil
}

// Query returns buckets covering [from, to) at the given period. Day and
// month boundaries follow loc. The bucket in progress is included.
func (r *Recorder) Query(from, to time.Time, period string, loc *time.Location) ([]Bucket, error) {
	if !to.After(from) {
		return nil, fmt.Errorf("the end must be after the start")
	}
	if to.Sub(from) > maxQueryRange {
		return nil, fmt.Errorf("ranges are limited to %d days", int(maxQueryRange.Hours()/24))
	}

	source, step := store.BucketStats1h, time.Hour
	if period == Period5m {
		source, step = store.BucketStats5m, bucketSize
	}
	var raw []*Bucket
	err := r.store.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(source).Cursor()
		end := store.TimeKey(to)
		for k, v := c.Seek(store.TimeKey(from.Truncate(step))); k != nil && string(k) < string(end); k, v = c.Next() {
			b := newBucket(time.Time{})
			if err := json.Unmarshal(v, b); err != nil {
				return err
			}
			raw = append(raw, b)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Add the live bucket so today's numbers are current.
	r.mu.Lock()
	if r.current != nil && r.current.Seconds > 0 && !r.current.Start.Before(from.Truncate(step)) && r.current.Start.Before(to) {
		live := newBucket(r.current.Start)
		live.merge(r.current)
		if period != Period5m {
			live.Start = live.Start.Truncate(time.Hour)
		}
		raw = append(raw, live)
	}
	r.mu.Unlock()

	key := func(t time.Time) time.Time {
		switch period {
		case Period5m:
			return t.Truncate(bucketSize)
		case PeriodHour:
			return t.Truncate(time.Hour)
		case PeriodDay:
			l := t.In(loc)
			return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
		case PeriodMonth:
			l := t.In(loc)
			return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, loc)
		}
		return t
	}
	switch period {
	case Period5m, PeriodHour, PeriodDay, PeriodMonth:
	default:
		return nil, fmt.Errorf("period must be 5m, hour, day or month")
	}

	var out []Bucket
	index := map[int64]int{}
	for _, b := range raw {
		k := key(b.Start)
		if k.Before(key(from)) && period != Period5m {
			continue
		}
		i, ok := index[k.Unix()]
		if !ok {
			out = append(out, *newBucket(k))
			i = len(out) - 1
			index[k.Unix()] = i
		}
		out[i].merge(b)
	}
	return out, nil
}

// Totals sums energy over [from, to).
func (r *Recorder) Totals(from, to time.Time) (map[string]float64, error) {
	buckets, err := r.Query(from, to, PeriodHour, time.UTC)
	if err != nil {
		return nil, err
	}
	totals := map[string]float64{GridImport: 0, GridExport: 0, PV: 0, Genset: 0, Load: 0}
	for _, b := range buckets {
		for k, v := range b.Energy {
			totals[k] += v
		}
	}
	return totals, nil
}
