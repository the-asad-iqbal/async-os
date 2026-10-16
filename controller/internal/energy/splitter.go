// Package energy splits a dual-source meter's energy into grid and genset
// totals.
//
// A meter at the point where the grid and, through an ATS, a genset feed
// the site sees both sources. Two things make its counters unreliable on
// their own: they must be attributed to whichever source is live, and on
// some meters the kWh counters freeze for the whole genset window while live
// power keeps updating.
//
// The splitter therefore does both:
//
//   - Every counter increment goes to the source that is live when it
//     happens. Import and export are tracked independently.
//   - While the genset runs, live power is integrated. When the genset
//     window closes, whatever the counters did not capture during the window
//     is topped up into the genset totals, so energy is neither lost nor
//     counted twice.
//
// Guards: counters going backwards or jumping at an implausible rate are
// re-based without accumulating; power gaps longer than MaxSampleGap are
// skipped so an outage cannot invent energy; power readings are clamped to
// MaxPowerW.
package energy

import (
	"math"
	"time"
)

// Defaults, taken from a site running this logic in production.
const (
	DefaultMaxPowerW    = 500_000
	DefaultMaxSampleGap = 10 * time.Minute
	// Counter changes smaller than this (kWh) are held until they add up.
	minDeltaKWh = 0.0001
)

// Totals are the four cumulative registers of a dual-source meter, in kWh.
type Totals struct {
	GridImportKWh   float64 `json:"grid_import_kwh"`
	GridExportKWh   float64 `json:"grid_export_kwh"`
	GensetImportKWh float64 `json:"genset_import_kwh"`
	GensetExportKWh float64 `json:"genset_export_kwh"`
}

// Sample is one poll of the meter. Nil fields were not read.
type Sample struct {
	Time time.Time
	// GensetRunning is the running signal: true when the genset, not the
	// grid, feeds the meter.
	GensetRunning bool
	// PowerW is signed: positive when energy flows in the import direction.
	PowerW    *float64
	ImportKWh *float64
	ExportKWh *float64
}

// Splitter accumulates Totals from samples. Not safe for concurrent use.
type Splitter struct {
	MaxPowerW    float64
	MaxSampleGap time.Duration

	totals Totals
	imp    counter
	exp    counter

	window windowState
}

type counter struct {
	set  bool
	last float64
	at   time.Time
}

type windowState struct {
	active      bool
	powerAt     time.Time
	importWh    float64
	exportWh    float64
	importStart *float64
	exportStart *float64
	// Counter values at the last sample taken while the genset ran. The
	// sample that closes the window belongs to the grid.
	importLast *float64
	exportLast *float64
}

// NewSplitter returns a splitter that continues from totals (restored after
// a restart). Counter baselines always start fresh: a restart re-bases
// rather than guessing which source the missed increments belonged to.
func NewSplitter(totals Totals) *Splitter {
	return &Splitter{MaxPowerW: DefaultMaxPowerW, MaxSampleGap: DefaultMaxSampleGap, totals: totals}
}

// Totals returns the accumulated registers.
func (s *Splitter) Totals() Totals { return s.totals }

// GensetWindowOpen reports whether a genset window is being integrated.
func (s *Splitter) GensetWindowOpen() bool { return s.window.active }

// Observe processes one sample.
func (s *Splitter) Observe(x Sample) {
	// 1. Counter increments, routed by the source live now.
	if x.ImportKWh != nil {
		if d := s.imp.delta(*x.ImportKWh, x.Time, s.MaxPowerW); d > 0 {
			if x.GensetRunning {
				s.totals.GensetImportKWh += d
			} else {
				s.totals.GridImportKWh += d
			}
		}
	}
	if x.ExportKWh != nil {
		if d := s.exp.delta(*x.ExportKWh, x.Time, s.MaxPowerW); d > 0 {
			if x.GensetRunning {
				s.totals.GensetExportKWh += d
			} else {
				s.totals.GridExportKWh += d
			}
		}
	}

	// 2. Genset window transitions.
	switch {
	case x.GensetRunning && !s.window.active:
		s.window = windowState{active: true, importStart: x.ImportKWh, exportStart: x.ExportKWh}
	case !x.GensetRunning && s.window.active:
		s.closeWindow()
	}
	if s.window.active {
		if x.ImportKWh != nil {
			s.window.importLast = x.ImportKWh
		}
		if x.ExportKWh != nil {
			s.window.exportLast = x.ExportKWh
		}
	}

	// 3. Integrate live power while the genset runs.
	if s.window.active && x.PowerW != nil {
		s.integrate(*x.PowerW, x.Time)
	}
}

// delta returns the accepted counter increase since the last accepted
// reading, or 0. Small changes are held until they add up.
func (c *counter) delta(value float64, at time.Time, maxPowerW float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if !c.set {
		c.set, c.last, c.at = true, value, at
		return 0
	}
	d := value - c.last
	if d < 0 {
		// Meter reset or replaced: re-base.
		c.last, c.at = value, at
		return 0
	}
	if d <= minDeltaKWh {
		return 0
	}
	if elapsed := at.Sub(c.at).Hours(); elapsed > 0 && d/elapsed*1000 > maxPowerW {
		// Implausible rate: a reset or a bad read, not real energy. A long
		// gap with a plausible rate is fine: the counter kept counting.
		c.last, c.at = value, at
		return 0
	}
	c.last, c.at = value, at
	return d
}

func (s *Splitter) integrate(powerW float64, at time.Time) {
	if math.IsNaN(powerW) {
		return
	}
	if s.window.powerAt.IsZero() {
		s.window.powerAt = at
		return
	}
	dt := at.Sub(s.window.powerAt)
	s.window.powerAt = at
	if dt <= 0 || dt > s.MaxSampleGap {
		return
	}
	p := math.Max(-s.MaxPowerW, math.Min(s.MaxPowerW, powerW))
	wh := math.Abs(p) * dt.Hours()
	if p >= 0 {
		s.window.importWh += wh
	} else {
		s.window.exportWh += wh
	}
}

// closeWindow tops up the genset totals with energy the counters missed.
func (s *Splitter) closeWindow() {
	w := s.window
	s.window = windowState{}

	captured := func(start, end *float64) float64 {
		if start == nil || end == nil {
			return 0
		}
		return math.Max(0, *end-*start)
	}
	if topUp := w.importWh/1000 - captured(w.importStart, w.importLast); topUp > minDeltaKWh {
		s.totals.GensetImportKWh += topUp
	}
	if topUp := w.exportWh/1000 - captured(w.exportStart, w.exportLast); topUp > minDeltaKWh {
		s.totals.GensetExportKWh += topUp
	}
}
