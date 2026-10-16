package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/the-asad-iqbal/async-os/controller/internal/auth"
	"github.com/the-asad-iqbal/async-os/controller/internal/control"
	"github.com/the-asad-iqbal/async-os/controller/internal/devices"
	"github.com/the-asad-iqbal/async-os/controller/internal/plant"
	"github.com/the-asad-iqbal/async-os/controller/internal/stats"
)

func (s *Server) getSnapshot(w http.ResponseWriter, _ *http.Request, _ *auth.User) error {
	writeJSON(w, http.StatusOK, s.Engine.Snapshot())
	return nil
}

// live streams snapshots as server-sent events. The session is re-checked
// periodically, so a revoked user stops receiving data.
func (s *Server) live(w http.ResponseWriter, r *http.Request, _ *auth.User) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errorf(http.StatusInternalServerError, "streaming unsupported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(snap plant.Snapshot) error {
		data, err := json.Marshal(snap)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	if err := send(s.Engine.Snapshot()); err != nil {
		return nil
	}

	updates, stop := s.Engine.Subscribe()
	defer stop()
	recheck := time.NewTicker(time.Minute)
	defer recheck.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case snap := <-updates:
			if err := send(snap); err != nil {
				return nil
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		case <-recheck.C:
			u, err := s.principal(r)
			if err != nil || !u.Role.Can(auth.PermDashboardView) {
				fmt.Fprint(w, "event: signed_out\ndata: {}\n\n")
				flusher.Flush()
				return nil
			}
		}
	}
}

func (s *Server) getSystem(w http.ResponseWriter, _ *http.Request, _ *auth.User) error {
	hostname, _ := os.Hostname()
	osName := "AsyncOS"
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				osName = strings.Trim(v, `"`)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    s.Version,
		"os":         osName,
		"hostname":   hostname,
		"started_at": s.started.UTC(),
		"uptime_s":   int(time.Since(s.started).Seconds()),
	})
	return nil
}

func (s *Server) siteLocation() *time.Location {
	loc, err := time.LoadLocation(s.Engine.Site().Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (s *Server) getEnergy(w http.ResponseWriter, r *http.Request, _ *auth.User) error {
	q := r.URL.Query()
	from, err := time.Parse(time.RFC3339, q.Get("from"))
	if err != nil {
		return errorf(http.StatusBadRequest, "from must be an RFC 3339 time")
	}
	to, err := time.Parse(time.RFC3339, q.Get("to"))
	if err != nil {
		return errorf(http.StatusBadRequest, "to must be an RFC 3339 time")
	}
	period := q.Get("period")
	if period == "" {
		period = stats.PeriodHour
	}
	buckets, err := s.Stats.Query(from, to, period, s.siteLocation())
	if err != nil {
		return errorf(http.StatusBadRequest, "%v", err)
	}
	if buckets == nil {
		buckets = []stats.Bucket{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"period": period, "timezone": s.siteLocation().String(), "buckets": buckets})
	return nil
}

// Summary is energy over a period with the derived ratios the dashboard
// shows.
type Summary struct {
	From   time.Time          `json:"from"`
	To     time.Time          `json:"to"`
	Energy map[string]float64 `json:"energy_wh"`
	// SelfConsumption is the share of PV energy used on site.
	SelfConsumption *float64 `json:"self_consumption"`
	// SelfSufficiency is the share of load covered by PV.
	SelfSufficiency *float64 `json:"self_sufficiency"`
}

func (s *Server) summary(from, to time.Time) (Summary, error) {
	totals, err := s.Stats.Totals(from, to)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{From: from, To: to, Energy: totals}
	if pv := totals[stats.PV]; pv > 0 {
		v := math.Max(0, math.Min(1, (pv-totals[stats.GridExport])/pv))
		out.SelfConsumption = &v
	}
	if load := totals[stats.Load]; load > 0 {
		v := math.Max(0, math.Min(1, (load-totals[stats.GridImport]-totals[stats.Genset])/load))
		out.SelfSufficiency = &v
	}
	return out, nil
}

func (s *Server) getEnergySummary(w http.ResponseWriter, _ *http.Request, _ *auth.User) error {
	loc := s.siteLocation()
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	year := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
	end := now.Add(time.Minute)

	out := map[string]Summary{}
	for name, from := range map[string]time.Time{"today": today, "month": month, "year": year} {
		sum, err := s.summary(from, end)
		if err != nil {
			return err
		}
		out[name] = sum
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) getSite(w http.ResponseWriter, _ *http.Request, _ *auth.User) error {
	writeJSON(w, http.StatusOK, s.Engine.Site())
	return nil
}

func (s *Server) getProfiles(w http.ResponseWriter, _ *http.Request, _ *auth.User) error {
	writeJSON(w, http.StatusOK, s.Library.All())
	return nil
}

// applySite validates, activates and persists a new configuration.
func (s *Server) applySite(site plant.Site) error {
	if err := s.Engine.Apply(site); err != nil {
		return errorf(http.StatusBadRequest, "%v", err)
	}
	return s.SaveSite(site)
}

func (s *Server) putSite(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var req struct {
		Name                    string               `json:"name"`
		Timezone                string               `json:"timezone"`
		Buses                   []plant.Bus          `json:"buses"`
		Devices                 []plant.DeviceConfig `json:"devices"`
		OperatorExportLimitMaxW float64              `json:"operator_export_limit_max_w"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	s.siteMu.Lock()
	defer s.siteMu.Unlock()
	site := s.Engine.Site()
	site.Name, site.Timezone, site.Buses, site.Devices = req.Name, req.Timezone, req.Buses, req.Devices
	site.OperatorExportLimitMaxW = req.OperatorExportLimitMaxW
	if site.Buses == nil {
		site.Buses = []plant.Bus{}
	}
	if site.Devices == nil {
		site.Devices = []plant.DeviceConfig{}
	}
	err := s.applySite(site)
	s.record(r, u, "site.update", site.Name, map[string]any{"buses": len(site.Buses), "devices": len(site.Devices)}, err)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, site)
	return nil
}

func (s *Server) putControl(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var settings control.Settings
	if err := decode(w, r, &settings); err != nil {
		return err
	}
	s.siteMu.Lock()
	defer s.siteMu.Unlock()
	site := s.Engine.Site()
	before := site.Control
	site.Control = settings
	err := s.applySite(site)
	s.record(r, u, "control.update", "", map[string]any{"before": before, "after": settings}, err)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, site.Control)
	return nil
}

// putExportLimit lets operators move the export limit within the bound an
// installer set. Installers and owners are bounded only by validation.
func (s *Server) putExportLimit(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var req struct {
		ExportLimitW float64 `json:"export_limit_w"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	s.siteMu.Lock()
	defer s.siteMu.Unlock()
	site := s.Engine.Site()
	if !u.Role.Can(auth.PermControlConfigure) && (req.ExportLimitW < 0 || req.ExportLimitW > site.OperatorExportLimitMaxW) {
		err := errorf(http.StatusForbidden, "operators may set 0 to %.0f W", site.OperatorExportLimitMaxW)
		s.record(r, u, "control.export_limit", "", map[string]any{"requested_w": req.ExportLimitW}, err)
		return err
	}
	before := site.Control.ExportLimitW
	site.Control.ExportLimitW = req.ExportLimitW
	err := s.applySite(site)
	s.record(r, u, "control.export_limit", "", map[string]any{"before_w": before, "after_w": req.ExportLimitW}, err)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, site.Control)
	return nil
}

// AlarmView is a live alarm with its acknowledgement.
type AlarmView struct {
	control.Alarm
	Key   string `json:"key"`
	Acked *Ack   `json:"acked,omitempty"`
}

func alarmKey(a control.Alarm) string { return a.Code + "/" + a.Device }

func (s *Server) currentAlarms() []AlarmView {
	snap := s.Engine.Snapshot()
	s.acksMu.Lock()
	defer s.acksMu.Unlock()
	active := map[string]bool{}
	out := []AlarmView{}
	for _, a := range snap.Decision.Alarms {
		k := alarmKey(a)
		active[k] = true
		v := AlarmView{Alarm: a, Key: k}
		if ack, ok := s.acks[k]; ok {
			v.Acked = &ack
		}
		out = append(out, v)
	}
	// Acknowledgements end with the condition they acknowledged.
	if !snap.Time.IsZero() {
		for k := range s.acks {
			if !active[k] {
				delete(s.acks, k)
			}
		}
	}
	return out
}

func (s *Server) getAlarms(w http.ResponseWriter, _ *http.Request, _ *auth.User) error {
	writeJSON(w, http.StatusOK, s.currentAlarms())
	return nil
}

func (s *Server) ackAlarm(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var req struct {
		Key string `json:"key"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	found := false
	for _, a := range s.currentAlarms() {
		if a.Key == req.Key {
			found = true
		}
	}
	if !found {
		return errorf(http.StatusNotFound, "no active alarm %q", req.Key)
	}
	s.acksMu.Lock()
	s.acks[req.Key] = Ack{By: u.Username, Time: time.Now().UTC()}
	s.acksMu.Unlock()
	s.record(r, u, "alarm.ack", req.Key, nil, nil)
	writeJSON(w, http.StatusOK, s.currentAlarms())
	return nil
}

func (s *Server) probe(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var req struct {
		Bus    plant.Bus          `json:"bus"`
		Device plant.DeviceConfig `json:"device"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if req.Bus.TimeoutMS <= 0 {
		req.Bus.TimeoutMS = 1000
	}
	reading, controllable, err := s.Engine.Probe(ctx, req.Bus, req.Device)
	s.record(r, u, "commissioning.probe", req.Device.Profile, map[string]any{"bus": req.Bus.ID, "unit_id": req.Device.UnitID}, err)
	if err != nil {
		return errorf(http.StatusBadGateway, "device did not answer: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"reading": reading, "controllable": controllable})
	return nil
}

func (s *Server) readRaw(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var req struct {
		Bus      plant.Bus     `json:"bus"`
		UnitID   int           `json:"unit_id"`
		Table    devices.Table `json:"table"`
		Address  uint16        `json:"address"`
		Quantity uint16        `json:"quantity"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	if req.Table != devices.TableHolding && req.Table != devices.TableInput {
		return errorf(http.StatusBadRequest, "table must be holding or input")
	}
	if req.Quantity < 1 || req.Quantity > 125 {
		return errorf(http.StatusBadRequest, "quantity must be 1 to 125")
	}
	if req.Bus.TimeoutMS <= 0 {
		req.Bus.TimeoutMS = 1000
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	values, err := s.Engine.ReadRaw(ctx, req.Bus, req.UnitID, req.Table, req.Address, req.Quantity)
	s.record(r, u, "commissioning.read", strconv.Itoa(req.UnitID), map[string]any{"table": req.Table, "address": req.Address, "quantity": req.Quantity}, err)
	if err != nil {
		return errorf(http.StatusBadGateway, "read failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": values})
	return nil
}

func (s *Server) getAudit(w http.ResponseWriter, r *http.Request, _ *auth.User) error {
	before, _ := strconv.ParseUint(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	entries, err := s.Audit.List(before, limit)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, entries)
	return nil
}

func (s *Server) reboot(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	if s.Reboot == nil {
		return errorf(http.StatusNotImplemented, "reboot is not available")
	}
	s.record(r, u, "system.reboot", "", nil, nil)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "rebooting"})
	go func() {
		time.Sleep(time.Second)
		if err := s.Reboot(); err != nil {
			s.Log.Error("reboot failed", "error", err)
		}
	}()
	return nil
}
