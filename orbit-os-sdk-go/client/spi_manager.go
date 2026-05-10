package client

import (
	"context"
	"fmt"

	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	spiv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/spi_service/v26"
)

// SpiManager wraps the SPI-related RPCs of the SpiService gRPC client.
type SpiManager struct {
	client spiv26.SpiServiceClient
	ctx    context.Context
}

// NewSpiManager creates a new SpiManager.
func NewSpiManager(client spiv26.SpiServiceClient, ctx context.Context) *SpiManager {
	return &SpiManager{client: client, ctx: ctx}
}

// ─── Public SDK types ─────────────────────────────────────────────────────────

// SpiConfig holds the configuration of a SPI device (SDK type).
type SpiConfig struct {
	Bus         uint32
	ChipSelect  uint32
	MaxSpeedHz  uint32
	BitsPerWord uint32
	Mode        int   // 0-3 (CPOL/CPHA)
	LSBFirst    bool
}

// ─── API ──────────────────────────────────────────────────────────────────────

// ListDevices returns the list of available SPI devices (e.g. ["spidev0.0", "spidev0.1"]).
func (m *SpiManager) ListDevices() ([]string, error) {
	resp, err := m.client.ListSpiBuses(m.ctx, &common.Void{})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("%s", resp.GetError().GetMessage())
	}
	return resp.GetDevices(), nil
}

// GetConfig returns the current configuration of a SPI device.
func (m *SpiManager) GetConfig(bus, cs uint32) (*SpiConfig, error) {
	resp, err := m.client.GetSpiConfig(m.ctx, &spiv26.SpiBusRequest{
		Bus:        bus,
		ChipSelect: cs,
	})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("%s", resp.GetError().GetMessage())
	}
	return &SpiConfig{
		Bus:         resp.GetBus(),
		ChipSelect:  resp.GetChipSelect(),
		MaxSpeedHz:  resp.GetMaxSpeedHz(),
		BitsPerWord: resp.GetBitsPerWord(),
		Mode:        int(resp.GetMode()),
		LSBFirst:    resp.GetLsbFirst(),
	}, nil
}

// SetConfig applies configuration to a SPI device.
func (m *SpiManager) SetConfig(cfg SpiConfig) error {
	mode := spiv26.SpiMode(cfg.Mode)
	resp, err := m.client.SetSpiConfig(m.ctx, &spiv26.SpiConfigRequest{
		Bus:         cfg.Bus,
		ChipSelect:  cfg.ChipSelect,
		MaxSpeedHz:  cfg.MaxSpeedHz,
		BitsPerWord: cfg.BitsPerWord,
		LsbFirst:    cfg.LSBFirst,
		Mode:        mode,
	})
	if err != nil {
		return err
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return fmt.Errorf("%s", resp.GetError().GetMessage())
	}
	return nil
}

// Transfer performs a full-duplex SPI transfer.
// dataOut: bytes to send on MOSI.
// readLength: how many MISO bytes the server returns (0 = write-only). When smaller than
// the transfer length, Gravity returns the *last* readLength bytes (typical for command+response).
// Returns the received bytes (nil for write-only).
func (m *SpiManager) Transfer(bus, cs uint32, dataOut []byte, readLength uint32) ([]byte, error) {
	resp, err := m.client.SpiTransfer(m.ctx, &spiv26.SpiTransferRequest{
		Bus:        bus,
		ChipSelect: cs,
		DataOut:    dataOut,
		ReadLength: readLength,
	})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("%s", resp.GetError().GetMessage())
	}
	return resp.GetDataIn(), nil
}

// ─── Internal helpers (not part of public API — use Transfer directly) ────────

func (m *SpiManager) write(bus, cs uint32, data []byte) error {
	_, err := m.Transfer(bus, cs, data, 0)
	return err
}

func (m *SpiManager) read(bus, cs uint32, n uint32) ([]byte, error) {
	return m.Transfer(bus, cs, nil, n)
}

func (m *SpiManager) writeRead(bus, cs uint32, dataOut []byte) ([]byte, error) {
	return m.Transfer(bus, cs, dataOut, uint32(len(dataOut)))
}
