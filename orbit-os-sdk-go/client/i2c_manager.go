package client

import (
	"context"
	"fmt"

	common "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	i2cv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/i2c_service/v26"

	"google.golang.org/grpc"
)

// ─── I2C Types ────────────────────────────────────────────────────────────────

// I2CConfig holds the configuration of an I2C bus.
type I2CConfig struct {
	Bus             uint32
	ClockHz         uint32
	TenBitAddr      bool
	ClockStretching bool
}

// I2CTransferRequest describes an I2C operation sent via the SDK.
//
// Operation is inferred from the fields:
//   - Data only (ReadLength=0)        → write
//   - ReadLength only (len(Data)=0)   → read
//   - Data + ReadLength               → write-then-read (repeated START)
//
// Note: register address (if needed) is the first byte(s) of Data.
type I2CTransferRequest struct {
	Bus        uint32
	Address    uint32 // 7-bit device address (e.g. 0x48)
	Data       []byte // bytes to write
	ReadLength uint32 // bytes to read back
	Flags      uint32 // extra i2c_msg flags (0 for most uses)
}

// ─── Manager ──────────────────────────────────────────────────────────────────

// I2CManager provides gRPC-based access to I2C buses on the device.
type I2CManager struct {
	client i2cv26.I2CServiceClient
	ctx    context.Context
}

// NewI2CManager creates a new I2CManager backed by the given gRPC client.
func NewI2CManager(client i2cv26.I2CServiceClient, ctx context.Context) *I2CManager {
	return &I2CManager{client: client, ctx: ctx}
}

// ─── API ──────────────────────────────────────────────────────────────────────

// ListBuses returns the bus numbers of all available I2C adapters on the device.
func (m *I2CManager) ListBuses(opts ...grpc.CallOption) ([]uint32, error) {
	resp, err := m.client.ListI2CBuses(m.ctx, &common.Void{}, opts...)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil && resp.Error.Code != 0 {
		return nil, fmt.Errorf("ListI2CBuses: %s", resp.Error.Message)
	}
	return resp.Buses, nil
}

// ScanBus probes all 7-bit addresses (0x03–0x77) and returns those that respond.
// Useful to discover which I2C devices are connected.
func (m *I2CManager) ScanBus(bus uint32, opts ...grpc.CallOption) ([]uint32, error) {
	resp, err := m.client.ScanI2CBus(m.ctx, &i2cv26.I2CBusRequest{Bus: bus}, opts...)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil && resp.Error.Code != 0 {
		return nil, fmt.Errorf("ScanI2CBus bus %d: %s", bus, resp.Error.Message)
	}
	return resp.Addresses, nil
}

// GetConfig returns the current configuration of the given I2C bus.
func (m *I2CManager) GetConfig(bus uint32, opts ...grpc.CallOption) (I2CConfig, error) {
	resp, err := m.client.GetI2CConfig(m.ctx, &i2cv26.I2CBusRequest{Bus: bus}, opts...)
	if err != nil {
		return I2CConfig{}, err
	}
	if resp.Error != nil && resp.Error.Code != 0 {
		return I2CConfig{}, fmt.Errorf("GetI2CConfig bus %d: %s", bus, resp.Error.Message)
	}
	return I2CConfig{
		Bus:             resp.Bus,
		ClockHz:         resp.ClockHz,
		TenBitAddr:      resp.TenBitAddr,
		ClockStretching: resp.ClockStretching,
	}, nil
}

// SetConfig applies configuration changes to the given bus.
// Note: only TenBitAddr can be changed at runtime; ClockHz requires a kernel
// driver reload and is stored for informational purposes only.
func (m *I2CManager) SetConfig(cfg I2CConfig, opts ...grpc.CallOption) error {
	resp, err := m.client.SetI2CConfig(m.ctx, &i2cv26.I2CConfigRequest{
		Bus:        cfg.Bus,
		ClockHz:    cfg.ClockHz,
		TenBitAddr: cfg.TenBitAddr,
	}, opts...)
	if err != nil {
		return err
	}
	if resp.Error != nil && resp.Error.Code != 0 {
		return fmt.Errorf("SetI2CConfig bus %d: %s", cfg.Bus, resp.Error.Message)
	}
	return nil
}

// Transfer performs an I2C operation:
//   - Write(bus, addr, data)                  → write bytes
//   - Read(bus, addr, n)                      → read n bytes
//   - WriteRead(bus, addr, data, n)           → write-then-read (repeated START)
//
// Returns the received bytes (nil for write-only operations).
func (m *I2CManager) Transfer(req I2CTransferRequest, opts ...grpc.CallOption) ([]byte, error) {
	resp, err := m.client.I2CTransfer(m.ctx, &i2cv26.I2CTransferRequest{
		Bus:        req.Bus,
		Address:    req.Address,
		Data:       req.Data,
		ReadLength: req.ReadLength,
		Flags:      req.Flags,
	}, opts...)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil && resp.Error.Code != 0 {
		return nil, fmt.Errorf("I2CTransfer bus %d addr 0x%02X: %s", req.Bus, req.Address, resp.Error.Message)
	}
	return resp.Data, nil
}

// ─── Internal helpers (not part of public API — use Transfer directly) ────────

func (m *I2CManager) write(bus, addr uint32, data []byte) error {
	_, err := m.Transfer(I2CTransferRequest{Bus: bus, Address: addr, Data: data})
	return err
}

func (m *I2CManager) read(bus, addr, n uint32) ([]byte, error) {
	return m.Transfer(I2CTransferRequest{Bus: bus, Address: addr, ReadLength: n})
}

func (m *I2CManager) writeRead(bus, addr uint32, writeData []byte, readLen uint32) ([]byte, error) {
	return m.Transfer(I2CTransferRequest{Bus: bus, Address: addr, Data: writeData, ReadLength: readLen})
}
