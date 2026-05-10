// Package profile defines the inverter profile schema and loader.
// A profile describes the UART config, Modbus slave address, poll interval,
// and the full register map for a specific inverter model.
package profile

import (
	"encoding/json"
	"fmt"
	"os"

	sdkclient "github.com/OrbitOS-org/orbit-os-sdk-go/v26/client"
)

// ValueType mirrors ESPHome modbus_controller value types.
type ValueType string

const (
	UWord   ValueType = "U_WORD"    // uint16, 1 register
	SWord   ValueType = "S_WORD"    // int16,  1 register
	UDword  ValueType = "U_DWORD"   // uint32, 2 registers, high word first
	UDwordR ValueType = "U_DWORD_R" // uint32, 2 registers, low word first (reversed)
)

// RegisterCount returns how many Modbus registers this value type occupies.
func (vt ValueType) RegisterCount() uint16 {
	if vt == UDword || vt == UDwordR {
		return 2
	}
	return 1
}

// Register describes a single Modbus register (or pair for DWORD types).
type Register struct {
	// Name is the machine-readable key used in the SSE payload and energy flow mapping.
	// Well-known names for the dashboard: pv_total_power, measured_power,
	// battery_power, total_consumption_power, battery_level, battery_health,
	// solar_energy_total, grid_import_total, grid_export_total,
	// battery_charged_total, battery_discharged_total.
	Name         string    `json:"name"`
	Label        string    `json:"label"`
	Address      uint16    `json:"address"`
	FunctionCode byte      `json:"function_code"` // 3 = holding, 4 = input
	ValueType    ValueType `json:"value_type"`
	Unit         string    `json:"unit"`
	// Multiplier is applied after decoding (e.g. 0.1 to convert raw 10ths to kWh).
	Multiplier float64 `json:"multiplier"`
}

// UartSettings holds the serial port configuration.
type UartSettings struct {
	BaudRate int    `json:"baud_rate"`
	DataBits int    `json:"data_bits"`
	Parity   string `json:"parity"` // "none", "even", "odd"
	StopBits int    `json:"stop_bits"`
}

// ModbusSettings holds Modbus-level parameters.
type ModbusSettings struct {
	SlaveAddress   byte `json:"slave_address"`
	PollIntervalMs int  `json:"poll_interval_ms"`
}

// DerivedRegister defines a reading computed from other named readings after a poll cycle.
// Currently only sum is supported: Value = sum of all named readings in SumOf.
type DerivedRegister struct {
	Name  string   `json:"name"`
	Label string   `json:"label"`
	Unit  string   `json:"unit"`
	SumOf []string `json:"sum_of"`
}

// Profile is the full configuration for one inverter model.
type Profile struct {
	Name      string            `json:"name"`
	Uart      UartSettings      `json:"uart"`
	Modbus    ModbusSettings    `json:"modbus"`
	Registers []Register        `json:"registers"`
	Derived   []DerivedRegister `json:"derived,omitempty"`
}

// UartConfig converts the profile's UART settings to the SDK type for the given port.
func (p *Profile) UartConfig(port string) sdkclient.UartConfig {
	parity := sdkclient.UartParityNone
	switch p.Uart.Parity {
	case "even":
		parity = sdkclient.UartParityEven
	case "odd":
		parity = sdkclient.UartParityOdd
	}
	stopBits := sdkclient.UartStopBits1
	if p.Uart.StopBits == 2 {
		stopBits = sdkclient.UartStopBits2
	}
	return sdkclient.UartConfig{
		Port:        port,
		Baudrate:    p.Uart.BaudRate,
		DataBits:    p.Uart.DataBits,
		Parity:      parity,
		StopBits:    stopBits,
		FlowControl: sdkclient.UartFlowNone,
	}
}

// Load parses a Profile from raw JSON bytes.
func Load(data []byte) (*Profile, error) {
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	if p.Modbus.PollIntervalMs <= 0 {
		p.Modbus.PollIntervalMs = 5000
	}
	return &p, nil
}

// LoadFile reads and parses a Profile from a JSON file on disk.
func LoadFile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile file %s: %w", path, err)
	}
	return Load(data)
}
