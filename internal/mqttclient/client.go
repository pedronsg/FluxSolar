package mqttclient

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/logger"
)

const logTag = "mqtt"

// Config holds MQTT broker connection settings, persisted to disk.
type Config struct {
	Enabled         bool   `json:"enabled"`
	Broker          string `json:"broker"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	ClientID        string `json:"client_id"`
	TopicPrefix     string `json:"topic_prefix"`
	DiscoveryPrefix string `json:"discovery_prefix"`
	DeviceName      string `json:"device_name"`
}

// DefaultConfig returns safe defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		Port:            1883,
		ClientID:        "fluxsolar",
		TopicPrefix:     "fluxsolar",
		DiscoveryPrefix: "homeassistant",
		DeviceName:      "FluxSolar",
	}
}

// Status is returned to the frontend.
type Status struct {
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
	LastSent  string `json:"last_sent,omitempty"`
}

// Payload is published to <topic_prefix>/state on every inverter poll.
type Payload struct {
	SolarW        *float64 `json:"solar_w"`
	LoadW         *float64 `json:"load_w"`
	GridW         *float64 `json:"grid_w"`
	BatteryW      *float64 `json:"battery_w"`
	BatterySOC    *float64 `json:"battery_soc"`
	BatteryHealth *float64 `json:"battery_health,omitempty"`

	// Cumulative energy totals for today (kWh) — for HA Energy Dashboard.
	SolarKWh        *float64 `json:"solar_kwh,omitempty"`
	LoadKWh         *float64 `json:"load_kwh,omitempty"`
	ImportKWh       *float64 `json:"import_kwh,omitempty"`
	ExportKWh       *float64 `json:"export_kwh,omitempty"`
	BChargedKWh     *float64 `json:"bcharged_kwh,omitempty"`
	BDischargedKWh  *float64 `json:"bdischarged_kwh,omitempty"`
}

func floatPtr(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

// MakePayload builds a Payload from raw register values (NaN = missing).
func MakePayload(solarW, loadW, gridW, battW, soc, health float64) Payload {
	return Payload{
		SolarW:        floatPtr(solarW),
		LoadW:         floatPtr(loadW),
		GridW:         floatPtr(gridW),
		BatteryW:      floatPtr(battW),
		BatterySOC:    floatPtr(soc),
		BatteryHealth: floatPtr(health),
	}
}

// AddTotals attaches today's cumulative kWh totals to an existing Payload.
func AddTotals(p *Payload, solar, load, importKWh, exportKWh, bcharged, bdischarged float64) {
	p.SolarKWh       = floatPtr(solar)
	p.LoadKWh        = floatPtr(load)
	p.ImportKWh      = floatPtr(importKWh)
	p.ExportKWh      = floatPtr(exportKWh)
	p.BChargedKWh    = floatPtr(bcharged)
	p.BDischargedKWh = floatPtr(bdischarged)
}

// Client manages the MQTT connection and HA discovery.
type Client struct {
	mu       sync.RWMutex
	cfg      Config
	mc       mqtt.Client
	status   Status
	lastSent time.Time
}

// New creates a Client. Call Start or Reconfigure to connect.
func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

// Start connects to the broker if enabled.
func (c *Client) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Enabled && c.cfg.Broker != "" {
		c.connectLocked()
	}
}

// Stop disconnects gracefully.
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mc != nil {
		c.mc.Disconnect(500)
		c.mc = nil
	}
	c.status.Connected = false
}

// Reconfigure stops the current connection and starts a new one.
func (c *Client) Reconfigure(cfg Config) {
	c.Stop()
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
	if cfg.Enabled && cfg.Broker != "" {
		c.mu.Lock()
		c.connectLocked()
		c.mu.Unlock()
	}
}

// Status returns a snapshot of the current connection state.
func (c *Client) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s := c.status
	if !c.lastSent.IsZero() {
		s.LastSent = c.lastSent.Format(time.RFC3339)
	}
	return s
}

// Publish sends a state payload to <topic_prefix>/state. No-op if not connected.
func (c *Client) Publish(p Payload) {
	c.mu.RLock()
	mc := c.mc
	connected := c.status.Connected
	prefix := c.cfg.TopicPrefix
	c.mu.RUnlock()

	if mc == nil || !connected {
		return
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	tok := mc.Publish(prefix+"/state", 0, true, b)
	tok.Wait()
	if tok.Error() == nil {
		c.mu.Lock()
		c.lastSent = time.Now()
		c.mu.Unlock()
	}
}

func (c *Client) connectLocked() {
	opts := mqtt.NewClientOptions()
	broker := fmt.Sprintf("tcp://%s:%d", c.cfg.Broker, c.cfg.Port)
	opts.AddBroker(broker)
	opts.SetClientID(c.cfg.ClientID)
	if c.cfg.Username != "" {
		opts.SetUsername(c.cfg.Username)
		opts.SetPassword(c.cfg.Password)
	}
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(10 * time.Second)

	opts.SetOnConnectHandler(func(mc mqtt.Client) {
		logger.Infof(logTag, "connected to %s", broker)
		c.mu.Lock()
		c.status.Connected = true
		c.status.Error = ""
		c.mu.Unlock()
		c.publishDiscovery(mc)
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		logger.Warnf(logTag, "connection lost: %v", err)
		c.mu.Lock()
		c.status.Connected = false
		c.status.Error = err.Error()
		c.mu.Unlock()
	})

	mc := mqtt.NewClient(opts)
	tok := mc.Connect()
	tok.Wait()
	if err := tok.Error(); err != nil {
		logger.Errorf(logTag, "connect to %s failed: %v", broker, err)
		c.status.Connected = false
		c.status.Error = err.Error()
	}
	c.mc = mc
}

// ── HA MQTT Discovery ─────────────────────────────────────────────────────────

type haDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Model        string   `json:"model"`
	Manufacturer string   `json:"manufacturer"`
}

type haConfig struct {
	Name              string   `json:"name"`
	UniqueID          string   `json:"unique_id"`
	StateTopic        string   `json:"state_topic"`
	ValueTemplate     string   `json:"value_template"`
	UnitOfMeasurement string   `json:"unit_of_measurement,omitempty"`
	DeviceClass       string   `json:"device_class,omitempty"`
	StateClass        string   `json:"state_class,omitempty"`
	Icon              string   `json:"icon,omitempty"`
	Device            haDevice `json:"device"`
}

func (c *Client) publishDiscovery(mc mqtt.Client) {
	c.mu.RLock()
	cfg := c.cfg
	c.mu.RUnlock()

	stateTopic := cfg.TopicPrefix + "/state"
	dev := haDevice{
		Identifiers:  []string{cfg.ClientID},
		Name:         cfg.DeviceName,
		Model:        "FluxSolar Monitor",
		Manufacturer: "FluxSolar",
	}

	sensors := []struct {
		id   string
		name string
		tmpl string
		unit string
		dc   string
		sc   string
		icon string
	}{
		{
			"solar_power", "Solar Power",
			"{{ value_json.solar_w | float(0) | round(1) }}", "W",
			"power", "measurement", "mdi:solar-power",
		},
		{
			"load_power", "Load Power",
			"{{ value_json.load_w | float(0) | round(1) }}", "W",
			"power", "measurement", "mdi:home-lightning-bolt",
		},
		{
			"grid_power", "Grid Power",
			"{{ value_json.grid_w | float(0) | round(1) }}", "W",
			"power", "measurement", "mdi:transmission-tower",
		},
		{
			"battery_power", "Battery Power",
			"{{ value_json.battery_w | float(0) | round(1) }}", "W",
			"power", "measurement", "mdi:battery-charging",
		},
		{
			"battery_soc", "Battery SOC",
			"{{ value_json.battery_soc | float(0) | round(1) }}", "%",
			"battery", "measurement", "",
		},
		{
			"battery_health", "Battery Health",
			"{{ value_json.battery_health | float(0) | round(1) }}", "%",
			"", "measurement", "mdi:battery-heart-variant",
		},
		// ── Energy sensors (kWh, total_increasing — for HA Energy Dashboard) ──
		{
			"solar_energy", "Solar Energy Today",
			"{{ value_json.solar_kwh | float(0) | round(3) }}", "kWh",
			"energy", "total_increasing", "mdi:solar-power",
		},
		{
			"load_energy", "Load Energy Today",
			"{{ value_json.load_kwh | float(0) | round(3) }}", "kWh",
			"energy", "total_increasing", "mdi:home-lightning-bolt",
		},
		{
			"grid_import_energy", "Grid Import Today",
			"{{ value_json.import_kwh | float(0) | round(3) }}", "kWh",
			"energy", "total_increasing", "mdi:transmission-tower",
		},
		{
			"grid_export_energy", "Grid Export Today",
			"{{ value_json.export_kwh | float(0) | round(3) }}", "kWh",
			"energy", "total_increasing", "mdi:transmission-tower-export",
		},
		{
			"battery_charged_energy", "Battery Charged Today",
			"{{ value_json.bcharged_kwh | float(0) | round(3) }}", "kWh",
			"energy", "total_increasing", "mdi:battery-arrow-up",
		},
		{
			"battery_discharged_energy", "Battery Discharged Today",
			"{{ value_json.bdischarged_kwh | float(0) | round(3) }}", "kWh",
			"energy", "total_increasing", "mdi:battery-arrow-down",
		},
	}

	for _, s := range sensors {
		uid := cfg.ClientID + "_" + s.id
		disc := haConfig{
			Name:              cfg.DeviceName + " " + s.name,
			UniqueID:          uid,
			StateTopic:        stateTopic,
			ValueTemplate:     s.tmpl,
			UnitOfMeasurement: s.unit,
			DeviceClass:       s.dc,
			StateClass:        s.sc,
			Icon:              s.icon,
			Device:            dev,
		}
		b, _ := json.Marshal(disc)
		topic := fmt.Sprintf("%s/sensor/%s/config", cfg.DiscoveryPrefix, uid)
		tok := mc.Publish(topic, 1, true, b)
		tok.Wait()
		if err := tok.Error(); err != nil {
			logger.Warnf(logTag, "discovery publish %s: %v", topic, err)
		} else {
			logger.Infof(logTag, "HA discovery: %s", topic)
		}
	}
}
