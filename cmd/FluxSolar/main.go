package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"fluxsolar/internal/history"
	"fluxsolar/internal/modbus"
	"fluxsolar/internal/mqttclient"
	"fluxsolar/internal/profile"
	"fluxsolar/internal/solar"

	apphub "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/app_hub_service/v26"
	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/client"
	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/logger"
	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/metadata"
)

const logTag = "main"

//go:embed metadata.json
var metadataJSON []byte

//go:embed orb/data/swatten.json
var defaultSwattenProfileJSON []byte

//go:embed orb/data/deye.json
var defaultDeyeProfileJSON []byte

//go:embed web/index.html
var dashboardHTML []byte

//go:embed web/profiles.html
var profilesHTML []byte

//go:embed web/mqtt.html
var mqttHTML []byte

//go:embed orb/icon.svg
var iconSVG []byte

var appManifest = metadata.MustParseAppManifestJSON(metadataJSON)

// ── SSE broker ───────────────────────────────────────────────────────────────

type broker struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newBroker() *broker { return &broker{clients: make(map[chan string]struct{})} }

func (b *broker) subscribe() chan string {
	ch := make(chan string, 8)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *broker) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

func (b *broker) broadcast(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default: // slow client — skip
		}
	}
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	host := flag.String("host", "192.168.5.226", "OrbitOS device IP address")
	uartPort := flag.String("uart", "", "UART port override (auto-detected from device if empty)")
	profileID := flag.String("profile", "swatten", "Inverter profile ID")
	profilesDir := flag.String("profiles-dir", "", "Directory with profile JSON files (one file per profile)")
	httpPort := flag.Int("http-port", 8080, "HTTP dashboard port")
	devMode := flag.Bool("dev", false, "enable developer UI (Profile Manager)")
	flag.Parse()

	resolvedProfilesDir, err := resolveProfilesDir(*profilesDir)
	if err != nil {
		logger.Fatalf(logTag, "resolve profiles dir: %v", err)
		os.Exit(1)
	}

	meta := metadata.Build(appManifest)
	logger.Init(meta.Name, "INFO", true)
	logger.Infof(logTag, "Starting %s", meta.Name)
	appManifest.PrintInfo()

	if err := ensureDefaultProfiles(resolvedProfilesDir); err != nil {
		logger.Fatalf(logTag, "prepare profiles dir: %v", err)
		os.Exit(1)
	}

	initCfgPath       := filepath.Join(resolvedProfilesDir, "init.cfg")
	mqttCfgPath       := filepath.Join(resolvedProfilesDir, "mqtt.cfg")
	activeProfilePath := filepath.Join(resolvedProfilesDir, "active_profile.cfg")

	historyDBPathFor := func(id string) string {
		return filepath.Join(resolvedProfilesDir, "history_"+id+".db")
	}

	// One-time migration: rename the legacy history.db → history_<profileID>.db.
	legacyHistoryDB := filepath.Join(resolvedProfilesDir, "history.db")

	mqttCfg := loadMQTTCfg(mqttCfgPath)
	mqttClient := mqttclient.New(mqttCfg)
	mqttClient.Start()
	defer mqttClient.Stop()

	// App-level context cancelled on SIGTERM/SIGINT.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// ── Load initial inverter profile ─────────────────────────────────────────
	// If --profile was not explicitly passed, restore the last used profile.
	profileFlagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "profile" {
			profileFlagSet = true
		}
	})
	if !profileFlagSet {
		if saved := readInitCfg(activeProfilePath); saved != "" {
			*profileID = saved
		}
	}

	initialProf, err := loadProfile(*profileID, resolvedProfilesDir)
	if err != nil {
		logger.Fatalf(logTag, "load profile %q: %v", *profileID, err)
		os.Exit(1)
	}
	logger.Infof(logTag, "Inverter profile: %s (slave 0x%02x, %d registers, poll %dms)",
		initialProf.Name, initialProf.Modbus.SlaveAddress, len(initialProf.Registers), initialProf.Modbus.PollIntervalMs)

	// Migrate legacy history.db → history_<profileID>.db (one-time, best-effort).
	if _, statErr := os.Stat(legacyHistoryDB); statErr == nil {
		targetDB := historyDBPathFor(*profileID)
		if _, statErr2 := os.Stat(targetDB); os.IsNotExist(statErr2) {
			if renErr := os.Rename(legacyHistoryDB, targetDB); renErr == nil {
				logger.Infof(logTag, "migrated history.db → %s", filepath.Base(targetDB))
			} else {
				logger.Warnf(logTag, "migrate history.db: %v", renErr)
			}
		}
	}

	// ── History store (per-profile, swappable) ────────────────────────────────
	var historyMu sync.RWMutex
	var historyStore *history.Store

	openHistoryStore := func(id string) *history.Store {
		path := historyDBPathFor(id)
		hs, hsErr := history.Open(path)
		if hsErr != nil {
			logger.Errorf(logTag, "open history db %s: %v — history disabled", path, hsErr)
			return nil
		}
		return hs
	}
	getHistoryStore := func() *history.Store {
		historyMu.RLock()
		defer historyMu.RUnlock()
		return historyStore
	}
	setHistoryStore := func(hs *history.Store) {
		historyMu.Lock()
		old := historyStore
		historyStore = hs
		historyMu.Unlock()
		if old != nil {
			old.Close()
		}
	}

	setHistoryStore(openHistoryStore(*profileID))
	defer func() { setHistoryStore(nil) }()

	// ── Connect to OrbitOS device (retry until context cancelled) ────────────
	var c *client.Client
	for {
		var connErr error
		c, connErr = client.NewClientAuto(*host)
		if connErr == nil {
			break
		}
		logger.Errorf(logTag, "connect to device: %v — retrying in 5s", connErr)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
	defer c.Close()

	hwModel, _ := c.SystemManager.GetHardwareModel()
	logger.Infof(logTag, "Device HW model: %s", hwModel)

	// ── Resolve UART port ─────────────────────────────────────────────────────
	portOverride := *uartPort
	if portOverride == "" {
		if saved := readInitCfg(initCfgPath); saved != "" {
			logger.Infof(logTag, "Using saved UART port from init.cfg: %s", saved)
			portOverride = saved
		}
	}
	resolvedPort, err := resolveUartPort(c.UartManager, portOverride)
	if err != nil {
		logger.Fatalf(logTag, "resolve UART port: %v", err)
		os.Exit(1)
	}
	logger.Infof(logTag, "Using UART port: %s", resolvedPort)

	// ── For RS485 on mini-UART: may need RTS/CTS control ────────────────────
	// Try to enable hardware flow control if available (some RS485 modules need this)
	logger.Infof(logTag, "Note: If using RS485, ensure RTS/CTS pins are connected for half-duplex control")

	// ── Open Modbus / UART (retry until context cancelled) ───────────────────
	// ── SSE broker + data store ───────────────────────────────────────────────
	hub := newBroker()
	var latestMu sync.RWMutex
	latest := solar.Data{Readings: map[string]solar.Reading{}}
	var stateMu sync.RWMutex
	activeProfileID := *profileID
	activeProfile := initialProf
	activePort := resolvedPort
	var mb *modbus.Client
	var pollCancel context.CancelFunc

	onData := func(d solar.Data) {
		latestMu.Lock()
		latest = d
		latestMu.Unlock()

		hs := getHistoryStore()
		if hs != nil && len(d.Readings) > 0 {
			hs.AddSample(d.Ts, history.Sample{
				SolarW:   readingValue(d, "pv_total_power"),
				LoadW:    readingValue(d, "total_consumption_power"),
				GridW:    readingValue(d, "measured_power"),
				BatteryW: readingValue(d, "battery_power"),
			})
		}

		b, _ := json.Marshal(d)
		hub.broadcast(string(b))

		p := mqttclient.MakePayload(
			readingValue(d, "pv_total_power"),
			readingValue(d, "total_consumption_power"),
			readingValue(d, "measured_power"),
			readingValue(d, "battery_power"),
			readingValue(d, "battery_level"),
			readingValue(d, "battery_health"),
		)
		if hs != nil {
			loc := time.Local
			now := time.Now().In(loc)
			dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			if t, err := hs.QueryTotals(context.Background(), dayStart, now); err == nil {
				mqttclient.AddTotals(&p, t.SolarKWh, t.LoadKWh, t.ImportKWh, t.ExportKWh, t.BChargedKWh, t.BDischargedKWh)
			}
		}
		mqttClient.Publish(p)

		logger.Infof(logTag, "poll complete — %d readings, err=%q", len(d.Readings), d.Error)
	}

	// ── Start/replace active profile runtime ─────────────────────────────────
	switchProfile := func(nextID string) error {
		nextProf, err := loadProfile(nextID, resolvedProfilesDir)
		if err != nil {
			return err
		}
		stateMu.RLock()
		port := activePort
		stateMu.RUnlock()

		stateMu.Lock()
		oldCancel := pollCancel
		oldMB := mb
		stateMu.Unlock()

		// Important: stop previous UART session before opening the next one on
		// the same port. Otherwise the old Close() can invalidate the new session.
		if oldCancel != nil {
			oldCancel()
		}
		if oldMB != nil {
			oldMB.Stop()
		}

		nextMB := modbus.NewClient(c.UartManager, port)
		if err := nextMB.Start(ctx, nextProf.UartConfig(port)); err != nil {
			return fmt.Errorf("start modbus on %s: %w", port, err)
		}

		pollCtx, cancel := context.WithCancel(ctx)
		stateMu.Lock()
		pollCancel = cancel
		mb = nextMB
		activeProfileID = nextID
		activeProfile = nextProf
		stateMu.Unlock()

		// Swap history store to the one for the new profile.
		setHistoryStore(openHistoryStore(nextID))

		poller := solar.NewPoller(nextMB, nextProf, onData)
		go poller.Run(pollCtx)

		logger.Infof(logTag, "Active profile: %s (%s) on %s @ %d baud",
			nextID, nextProf.Name, port, nextProf.Uart.BaudRate)
		return nil
	}

	// Do not block app startup on UART/Modbus bring-up.
	// Keep retrying in background.
	go func(initialID string) {
		for {
			if err := switchProfile(initialID); err == nil {
				return
			} else {
				logger.Errorf(logTag, "start profile %s failed: %v — retrying in 5s", initialID, err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}(*profileID)

	defer func() {
		stateMu.Lock()
		if pollCancel != nil {
			pollCancel()
		}
		if mb != nil {
			mb.Stop()
		}
		stateMu.Unlock()
	}()

	// ── HTTP server ───────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Wrap all handlers with logging middleware
	logHandler := func(name string, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			logger.Infof(logTag, "HTTP %s %s", r.Method, r.RequestURI)
			h(w, r)
		}
	}

	// Register API endpoints FIRST - more specific patterns first
	mux.HandleFunc("/api/data", logHandler("api/data", func(w http.ResponseWriter, r *http.Request) {
		latestMu.RLock()
		d := latest
		latestMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(d)
	}))

	mux.HandleFunc("/api/profile", logHandler("api/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid body, expected {\"id\":\"...\"}"})
				return
			}
			newID := strings.TrimSpace(req.ID)
			if err := switchProfile(newID); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if err := writeInitCfg(activeProfilePath, newID); err != nil {
				logger.Warnf(logTag, "save active profile: %v", err)
			}
		}
		stateMu.RLock()
		p := activeProfile
		currentID := activeProfileID
		stateMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      currentID,
			"profile": p,
		})
	}))

	mux.HandleFunc("/api/totals", logHandler("api/totals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		hs := getHistoryStore()
		if hs == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "history not available"})
			return
		}
		loc := time.Local
		now := time.Now().In(loc)
		var anchor time.Time
		if dateParam := strings.TrimSpace(r.URL.Query().Get("date")); dateParam != "" {
			parsed, err := time.ParseInLocation("2006-01-02", dateParam, loc)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid date %q (expected YYYY-MM-DD)", dateParam)})
				return
			}
			anchor = parsed
		} else {
			anchor = now
		}
		var start, end time.Time
		switch r.URL.Query().Get("period") {
		case "day":
			start = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, loc)
			end = start.AddDate(0, 0, 1)
		case "month":
			start = time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, loc)
			end = start.AddDate(0, 1, 0)
		case "year":
			start = time.Date(anchor.Year(), 1, 1, 0, 0, 0, 0, loc)
			end = start.AddDate(1, 0, 0)
		}
		totals, err := hs.QueryTotals(r.Context(), start, end)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(totals)
	}))

	mux.HandleFunc("/api/config", logHandler("api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"dev": *devMode})
	}))

	mux.HandleFunc("/api/version", logHandler("api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"version": appManifest.Version})
	}))

	mux.HandleFunc("/api/profiles", logHandler("api/profiles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := listProfiles(resolvedProfilesDir)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			stateMu.RLock()
			currentID := activeProfileID
			stateMu.RUnlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"current":     currentID,
				"profilesDir": resolvedProfilesDir,
				"profiles":    items,
				"canWrite":    resolvedProfilesDir != "",
				"builtinOnly": false,
			})
		case http.MethodPost:
			if !requireDev(w, *devMode) {
				return
			}
			var req struct {
				ID      string          `json:"id"`
				Profile profile.Profile `json:"profile"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid profile JSON"})
				return
			}
			if err := saveProfile(resolvedProfilesDir, req.ID, &req.Profile); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			// If the saved profile is currently active, reload it so the new
			// settings (e.g. poll_interval_ms) take effect immediately.
			savedID := strings.TrimSpace(req.ID)
			if savedID == "" {
				savedID = slugFromName(req.Profile.Name)
			}
			stateMu.RLock()
			currentID := activeProfileID
			stateMu.RUnlock()
			if savedID == currentID {
				if err := switchProfile(currentID); err != nil {
					logger.Warnf(logTag, "reload active profile after save: %v", err)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	mux.HandleFunc("/api/profiles/", logHandler("api/profiles/id", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
		if r.Method == http.MethodGet {
			p, err := loadProfile(id, resolvedProfilesDir)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(p)
			return
		}
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !requireDev(w, *devMode) {
			return
		}
		if err := deleteProfile(resolvedProfilesDir, id); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}))

	mux.HandleFunc("/api/ports", logHandler("api/ports", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				Port string `json:"port"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Port) == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid body, expected {\"port\":\"...\"}"})
				return
			}
			ports, err := c.UartManager.ListPorts()
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("list uart ports: %v", err)})
				return
			}
			nextPort := strings.TrimSpace(req.Port)
			ok := false
			for _, p := range ports {
				if p == nextPort {
					ok = true
					break
				}
			}
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("port %q not available", nextPort)})
				return
			}
			stateMu.Lock()
			activePort = nextPort
			currentID := activeProfileID
			stateMu.Unlock()
			if err := switchProfile(currentID); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if err := writeInitCfg(initCfgPath, nextPort); err != nil {
				logger.Warnf(logTag, "write init.cfg: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		logger.Infof(logTag, "api/ports: calling UartManager.ListPorts()")
		ports, err := c.UartManager.ListPorts()
		if err != nil {
			logger.Errorf(logTag, "api/ports: ListPorts() failed: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			stateMu.RLock()
			currentPort := activePort
			stateMu.RUnlock()
			fmt.Fprintf(w, `{"error":"list uart ports: %s","ports":[],"current":"%s"}`, err.Error(), currentPort)
			return
		}
		stateMu.RLock()
		currentPort := activePort
		stateMu.RUnlock()
		logger.Infof(logTag, "api/ports: success - %v (current=%s)", ports, currentPort)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ports":   ports,
			"current": currentPort,
		})
	}))

	mux.HandleFunc("/api/history", logHandler("api/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		hs := getHistoryStore()
		if hs == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "history database is not available"})
			return
		}
		rangeKind := strings.TrimSpace(r.URL.Query().Get("range"))
		if rangeKind == "" {
			rangeKind = "day"
		}
		dateParam := strings.TrimSpace(r.URL.Query().Get("date"))
		loc := time.Local
		var anchor time.Time
		if dateParam == "" {
			anchor = time.Now().In(loc)
		} else {
			parsed, err := time.ParseInLocation("2006-01-02", dateParam, loc)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid date %q (expected YYYY-MM-DD)", dateParam)})
				return
			}
			anchor = parsed
		}
		var (
			start, end time.Time
			gran       history.Granularity
		)
		switch rangeKind {
		case "day":
			start = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, loc)
			end = start.AddDate(0, 0, 1)
			gran = history.GranMinute
		case "month":
			start = time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, loc)
			end = start.AddDate(0, 1, 0)
			gran = history.GranHour
		case "year":
			start = time.Date(anchor.Year(), 1, 1, 0, 0, 0, 0, loc)
			end = start.AddDate(1, 0, 0)
			gran = history.GranDay
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid range %q (expected day|month|year)", rangeKind)})
			return
		}
		buckets, err := hs.Query(r.Context(), start, end, gran, loc)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"range":       rangeKind,
			"granularity": string(gran),
			"date":        anchor.Format("2006-01-02"),
			"start":       start,
			"end":         end,
			"timezone":    loc.String(),
			"buckets":     buckets,
		})
	}))

	mux.HandleFunc("/api/mqtt", logHandler("api/mqtt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			cfg := loadMQTTCfg(mqttCfgPath)
			cfg.Password = maskPassword(cfg.Password)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"config": cfg,
				"status": mqttClient.Status(),
			})
		case http.MethodPost:
			var cfg mqttclient.Config
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
				return
			}
			if cfg.Password == "••••••••" {
				existing := loadMQTTCfg(mqttCfgPath)
				cfg.Password = existing.Password
			}
			if err := saveMQTTCfg(mqttCfgPath, cfg); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			mqttClient.Reconfigure(cfg)
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	mux.HandleFunc("/api/mqtt/discover", logHandler("api/mqtt/discover", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !mqttClient.Status().Connected {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "MQTT not connected"})
			return
		}
		cfg := loadMQTTCfg(mqttCfgPath)
		mqttClient.Reconfigure(cfg)
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}))

	mux.HandleFunc("/mqtt", logHandler("mqtt-page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(mqttHTML)
	}))

	mux.HandleFunc("/events", logHandler("events", sseHandler(hub, func() solar.Data {
		latestMu.RLock()
		defer latestMu.RUnlock()
		return latest
	})))

	mux.HandleFunc("/profiles", logHandler("profiles-page", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/profiles" {
			http.NotFound(w, r)
			return
		}
		if !*devMode {
			http.Error(w, "403 Forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(profilesHTML)
	}))

	mux.HandleFunc("/favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(iconSVG)
	})

	// Catch-all for root LAST
	mux.HandleFunc("/", logHandler("root", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			logger.Infof(logTag, "404: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		logger.Infof(logTag, "serving dashboard")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(dashboardHTML)
	}))

	ln, actualPort := listenWithFallback(*httpPort)
	srv := &http.Server{Handler: mux}

	go func() {
		logger.Infof(logTag, "Dashboard at http://%s:%d", *host, actualPort)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Errorf(logTag, "http server: %v", err)
		}
	}()

	// ── Register with AppHub so the portal shows a link to the dashboard ─────
	if err := c.AppHubManager.RegisterService(&apphub.RegisterServiceRequest{
		Host:   "127.0.0.1",
		Port:   int32(actualPort),
		Routes: []*apphub.Route{{Path: "/fluxsolar"}},
		Health: &apphub.HealthCheck{
			Type: apphub.HealthCheckType_HEALTH_CHECK_TCP,
		},
	}); err != nil {
		logger.Errorf(logTag, "apphub register: %v", err)
	} else {
		logger.Infof(logTag, "Registered with AppHub at /fluxsolar -> 127.0.0.1:%d", actualPort)
	}
	defer c.AppHubManager.UnregisterService()

	<-ctx.Done()
	logger.Infof(logTag, "Shutting down…")

	// Close UART and databases immediately on SIGTERM, before the HTTP server
	// drains (which can block up to 5 s). This ensures the port is released and
	// all data is flushed before the update process starts the new instance.
	stateMu.Lock()
	if pollCancel != nil {
		pollCancel()
		pollCancel = nil
	}
	if mb != nil {
		mb.Stop()
		mb = nil
	}
	stateMu.Unlock()
	setHistoryStore(nil)

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}

// sseHandler returns an HTTP handler that streams solar.Data updates via SSE.
func sseHandler(hub *broker, getLatest func() solar.Data) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ch := hub.subscribe()
		defer hub.unsubscribe(ch)

		// Send the current snapshot immediately so the dashboard isn't blank.
		if b, err := json.Marshal(getLatest()); err == nil {
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}

		keepalive := time.NewTicker(25 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-keepalive.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}

// loadProfile resolves the profile from <profilesDir>/<id>.json.
func loadProfile(id, dir string) (*profile.Profile, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("missing profile id")
	}
	if dir == "" {
		return nil, fmt.Errorf("profiles-dir is empty")
	}
	p, err := profile.LoadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, fmt.Errorf("load profile %q: %w", id, err)
	}
	return p, nil
}

// listenWithFallback tries to bind on preferredPort; if that fails it lets the
// OS pick a free port. Returns the listener and the actual port number.
func listenWithFallback(preferredPort int) (net.Listener, int) {
	if ln, err := net.Listen("tcp", fmt.Sprintf(":%d", preferredPort)); err == nil {
		return ln, preferredPort
	}
	logger.Infof(logTag, "port %d in use, picking a free port", preferredPort)
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		logger.Fatalf(logTag, "could not bind HTTP listener: %v", err)
		os.Exit(1)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

// resolveUartPort returns the port to use for Modbus:
//   - If override is non-empty and present in the device list, use it.
//   - If override is empty and exactly one port is available, use that.
//   - If override is empty and multiple ports are available, default to the first one.
func resolveUartPort(um *client.UartManager, override string) (string, error) {
	ports, err := um.ListPorts()
	if err != nil {
		return "", fmt.Errorf("list UART ports: %w", err)
	}
	logger.Infof(logTag, "Available UART ports: %v", ports)

	if override != "" {
		for _, p := range ports {
			if p == override {
				return override, nil
			}
		}
		return "", fmt.Errorf("port %q not found on device (available: %v)", override, ports)
	}

	if len(ports) == 1 {
		logger.Infof(logTag, "Auto-selected UART port: %s", ports[0])
		return ports[0], nil
	}
	if len(ports) == 0 {
		return "", fmt.Errorf("no UART ports available on device")
	}

	// Neutral selection: pick the first available port as reported by the device.
	logger.Warnf(logTag, "Multiple UART ports available %v; defaulting to first: %s", ports, ports[0])
	return ports[0], nil
}

var profileIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type profileSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Source        string `json:"source"`
	RegisterCount int    `json:"register_count"`
}

func listProfiles(dir string) ([]profileSummary, error) {
	if dir == "" {
		return nil, fmt.Errorf("profiles-dir is empty")
	}
	items := []profileSummary{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []profileSummary{}, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p, perr := profile.LoadFile(filepath.Join(dir, e.Name()))
		if perr != nil {
			continue
		}
		items = append(items, profileSummary{
			ID:            strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
			Name:          p.Name,
			Source:        "file",
			RegisterCount: len(p.Registers),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func saveProfile(dir, requestedID string, p *profile.Profile) error {
	id := strings.TrimSpace(requestedID)
	if id == "" {
		id = slugFromName(p.Name)
	}
	if id == "" {
		return fmt.Errorf("profile id is required (or provide a valid name)")
	}
	if !profileIDPattern.MatchString(id) {
		return fmt.Errorf("profile id must match %s", profileIDPattern.String())
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("profile name is required")
	}
	if len(p.Registers) == 0 {
		return fmt.Errorf("profile must define at least one register")
	}
	if p.Uart.BaudRate <= 0 || p.Uart.DataBits <= 0 || p.Uart.StopBits <= 0 {
		return fmt.Errorf("invalid uart config")
	}
	switch strings.ToLower(strings.TrimSpace(p.Uart.Parity)) {
	case "none", "even", "odd":
	default:
		return fmt.Errorf(`uart.parity must be "none", "even", or "odd"`)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create profiles dir: %w", err)
	}
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profile: %w", err)
	}
	path := filepath.Join(dir, id+".json")
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("write profile file: %w", err)
	}
	return nil
}

func slugFromName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	return s
}

func deleteProfile(dir, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("missing profile id")
	}
	if !profileIDPattern.MatchString(id) {
		return fmt.Errorf("invalid profile id")
	}
	path := filepath.Join(dir, id+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("profile %q not found", id)
		}
		return fmt.Errorf("remove profile: %w", err)
	}
	return nil
}

func ensureDefaultProfiles(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("profiles-dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create profiles dir: %w", err)
	}

	// Seed swatten.json — with one-time migration from legacy locations.
	swattenPath := filepath.Join(dir, "swatten.json")
	if _, err := os.Stat(swattenPath); os.IsNotExist(err) {
		seeded := false
		for _, legacy := range []string{
			filepath.Clean("cmd/FluxSolar/profiles/swatten.json"),
			filepath.Clean("profiles/swatten.json"),
		} {
			b, err := os.ReadFile(legacy)
			if err != nil {
				continue
			}
			if err := os.WriteFile(swattenPath, append(bytes.TrimSpace(b), '\n'), 0o644); err != nil {
				return fmt.Errorf("migrate swatten profile from %s: %w", legacy, err)
			}
			seeded = true
			break
		}
		if !seeded {
			if err := os.WriteFile(swattenPath, append(defaultSwattenProfileJSON, '\n'), 0o644); err != nil {
				return fmt.Errorf("seed swatten profile: %w", err)
			}
		}
	}

	// Seed deye.json (always update so new installs and upgrades get the latest).
	deyePath := filepath.Join(dir, "deye.json")
	if _, err := os.Stat(deyePath); os.IsNotExist(err) {
		if err := os.WriteFile(deyePath, append(defaultDeyeProfileJSON, '\n'), 0o644); err != nil {
			return fmt.Errorf("seed deye profile: %w", err)
		}
	}

	return nil
}

// readingValue returns the value of the named reading in d, or NaN if missing.
func readingValue(d solar.Data, name string) float64 {
	if d.Readings == nil {
		return nan
	}
	r, ok := d.Readings[name]
	if !ok {
		return nan
	}
	return r.Value
}

var nan = func() float64 {
	zero := 0.0
	return zero / zero
}()

func resolveProfilesDir(flagValue string) (string, error) {
	if strings.TrimSpace(flagValue) != "" {
		return filepath.Clean(flagValue), nil
	}

	// OrbitOS convention: app writable data lives in orb/data.
	orbDataRepoPath := filepath.Clean("cmd/FluxSolar/orb/data")
	if st, err := os.Stat(orbDataRepoPath); err == nil && st.IsDir() {
		return orbDataRepoPath, nil
	}

	// Fallback when running from cmd/FluxSolar itself.
	orbDataLocalPath := filepath.Clean("orb/data")
	if st, err := os.Stat(orbDataLocalPath); err == nil && st.IsDir() {
		return orbDataLocalPath, nil
	}

	// Last resort: create in repository-style location.
	return orbDataRepoPath, nil
}

func requireDev(w http.ResponseWriter, dev bool) bool {
	if dev {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "developer mode not enabled; restart with --dev"})
	return false
}

func readInitCfg(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeInitCfg(path, port string) error {
	return os.WriteFile(path, []byte(port+"\n"), 0644)
}

func loadMQTTCfg(path string) mqttclient.Config {
	cfg := mqttclient.DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func saveMQTTCfg(path string, cfg mqttclient.Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func maskPassword(p string) string {
	if p == "" {
		return ""
	}
	return "••••••••"
}
