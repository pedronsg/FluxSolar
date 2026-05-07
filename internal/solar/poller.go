// Package solar provides the Data type and Poller that periodically reads all
// registers from the inverter via Modbus and broadcasts the result.
package solar

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
	"time"

	"fluxsolar/internal/modbus"
	"fluxsolar/internal/profile"

	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/logger"
)

const logTag = "solar"
const interRequestDelay = 50 * time.Millisecond

// Reading holds the decoded value for a single register.
type Reading struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Data is the snapshot produced after each full poll cycle.
type Data struct {
	Ts       time.Time          `json:"ts"`
	Readings map[string]Reading `json:"readings"`
	Error    string             `json:"error,omitempty"`
}

// Poller reads all profile registers on a fixed interval and notifies via onData.
type Poller struct {
	mb      *modbus.Client
	prof    *profile.Profile
	mu      sync.RWMutex
	latest  Data
	onData  func(Data)
}

func NewPoller(mb *modbus.Client, p *profile.Profile, onData func(Data)) *Poller {
	return &Poller{
		mb:     mb,
		prof:   p,
		onData: onData,
		latest: Data{Readings: map[string]Reading{}},
	}
}

// Run polls in a loop until ctx is cancelled. The first poll fires immediately.
func (p *Poller) Run(ctx context.Context) {
	interval := time.Duration(p.prof.Modbus.PollIntervalMs) * time.Millisecond
	next := time.Now()
	for {
		now := time.Now()
		if wait := next.Sub(now); wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}

		start := time.Now()
		p.poll()
		next = start.Add(interval)

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// Latest returns the most recently completed data snapshot (safe for concurrent reads).
func (p *Poller) Latest() Data {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.latest
}

func (p *Poller) poll() {
	data := Data{
		Ts:       time.Now(),
		Readings: make(map[string]Reading, len(p.prof.Registers)),
	}

	slaveID := p.prof.Modbus.SlaveAddress
	var lastErr string

	for _, reg := range p.prof.Registers {
		count := reg.ValueType.RegisterCount()
		regs, err := p.mb.ReadRegisters(slaveID, reg.FunctionCode, reg.Address, count)
		if err != nil {
			logger.Warnf(logTag, "read %s (addr %d): %v", reg.Name, reg.Address, err)
			lastErr = err.Error()
			// Stop this cycle on first comm error to avoid hammering UART.
			break
		}

		val := decodeValue(regs, reg.ValueType)
		if reg.Multiplier != 0 {
			val *= reg.Multiplier
		}

		data.Readings[reg.Name] = Reading{
			Label: reg.Label,
			Value: val,
			Unit:  reg.Unit,
		}

		time.Sleep(interRequestDelay) // inter-request delay for RS485 bus
	}

	// Compute derived readings (e.g. pv_total_power = pv1_total_power + pv2_total_power).
	for _, d := range p.prof.Derived {
		var sum float64
		for _, src := range d.SumOf {
			if r, ok := data.Readings[src]; ok {
				sum += r.Value
			}
		}
		data.Readings[d.Name] = Reading{
			Label: d.Label,
			Value: sum,
			Unit:  d.Unit,
		}
	}

	data.Error = lastErr

	p.mu.Lock()
	p.latest = data
	p.mu.Unlock()

	if p.onData != nil {
		p.onData(data)
	}
}

// decodeValue converts raw register words into a float64 according to the value type.
// U_DWORD_R (reversed): register[0] = low word, register[1] = high word — matches ESPHome U_DWORD_R.
func decodeValue(regs []uint16, vt profile.ValueType) float64 {
	switch vt {
	case profile.UWord:
		return float64(regs[0])
	case profile.SWord:
		return float64(int16(regs[0]))
	case profile.UDword:
		raw := binary.BigEndian.Uint32([]byte{
			byte(regs[0] >> 8), byte(regs[0]),
			byte(regs[1] >> 8), byte(regs[1]),
		})
		return float64(raw)
	case profile.UDwordR: // low word first
		lo := uint32(regs[0])
		hi := uint32(regs[1]) << 16
		return float64(hi | lo)
	}
	return math.NaN()
}
