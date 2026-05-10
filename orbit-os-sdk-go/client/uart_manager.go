package client

import (
	"context"
	"fmt"
	"io"

	types "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	uartv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/uart_service/v26"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// UartParity mirrors the proto UartParity enum
type UartParity int

const (
	UartParityNone UartParity = iota
	UartParityEven
	UartParityOdd
)

// UartStopBits mirrors the proto UartStopBits enum
type UartStopBits int

const (
	UartStopBits1 UartStopBits = iota
	UartStopBits2
)

// UartFlowControl mirrors the proto UartFlowControl enum
type UartFlowControl int

const (
	UartFlowNone     UartFlowControl = iota
	UartFlowHardware                 // RTS/CTS
	UartFlowSoftware                 // XON/XOFF
)

// UartConfig holds the configuration used to open a UART port
type UartConfig struct {
	Port        string
	Baudrate    int
	DataBits    int // 5, 6, 7 or 8
	Parity      UartParity
	StopBits    UartStopBits
	FlowControl UartFlowControl
}

// ─── Manager ──────────────────────────────────────────────────────────────────

// UartManager is the SDK client for UART operations
type UartManager struct {
	client uartv26.UartServiceClient
	ctx    context.Context
}

// NewUartManager creates a new UartManager
func NewUartManager(client uartv26.UartServiceClient, ctx context.Context) *UartManager {
	return &UartManager{client: client, ctx: ctx}
}

// ─── Methods ──────────────────────────────────────────────────────────────────

// ListPorts returns the names of available UART ports (e.g. "ttyAMA0")
func (m *UartManager) ListPorts() ([]string, error) {
	resp, err := m.client.ListUartPorts(m.ctx, &types.Void{})
	if err != nil {
		return nil, fmt.Errorf("ListUartPorts: %w", err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("ListUartPorts: %s", resp.Error.Message)
	}
	return resp.Ports, nil
}

// Open opens and configures a UART port on the device
func (m *UartManager) Open(cfg UartConfig) error {
	req := &uartv26.UartConfigRequest{
		Port:        cfg.Port,
		Baudrate:    int32(cfg.Baudrate),
		DataBits:    int32(cfg.DataBits),
		Parity:      sdkParityToProto(cfg.Parity),
		StopBits:    sdkStopBitsToProto(cfg.StopBits),
		FlowControl: sdkFlowControlToProto(cfg.FlowControl),
	}
	resp, err := m.client.OpenUart(m.ctx, req)
	if err != nil {
		return fmt.Errorf("OpenUart %s: %w", cfg.Port, err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return fmt.Errorf("OpenUart %s: %s", cfg.Port, resp.Error.Message)
	}
	return nil
}

// Close closes an open UART port on the device
func (m *UartManager) Close(port string) error {
	resp, err := m.client.CloseUart(m.ctx, &uartv26.UartPortRequest{Port: port})
	if err != nil {
		return fmt.Errorf("CloseUart %s: %w", port, err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return fmt.Errorf("CloseUart %s: %s", port, resp.Error.Message)
	}
	return nil
}

// GetConfig returns the current configuration of an open UART port
func (m *UartManager) GetConfig(port string) (*UartConfig, error) {
	resp, err := m.client.GetUartConfig(m.ctx, &uartv26.UartPortRequest{Port: port})
	if err != nil {
		return nil, fmt.Errorf("GetUartConfig %s: %w", port, err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("GetUartConfig %s: %s", port, resp.Error.Message)
	}
	return &UartConfig{
		Port:        resp.Port,
		Baudrate:    int(resp.Baudrate),
		DataBits:    int(resp.DataBits),
		Parity:      protoParityToSDK(resp.Parity),
		StopBits:    protoStopBitsToSDK(resp.StopBits),
		FlowControl: protoFlowControlToSDK(resp.FlowControl),
	}, nil
}

// Write sends bytes to an open UART port; returns the number of bytes written
func (m *UartManager) Write(port string, data []byte) (int, error) {
	resp, err := m.client.WriteUart(m.ctx, &uartv26.UartWriteRequest{Port: port, Data: data})
	if err != nil {
		return 0, fmt.Errorf("WriteUart %s: %w", port, err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return 0, fmt.Errorf("WriteUart %s: %s", port, resp.Error.Message)
	}
	return int(resp.BytesWritten), nil
}

// Listen starts a server-streaming read from a UART port.
// onChunk is called for each received chunk of data.
// Blocks until ctx is cancelled, the server closes the stream, or an error occurs.
func (m *UartManager) Listen(ctx context.Context, port string, maxChunkSize int, onChunk func([]byte)) error {
	req := &uartv26.UartReadRequest{
		Port:         port,
		MaxChunkSize: int32(maxChunkSize),
	}
	stream, err := m.client.ListenUart(ctx, req)
	if err != nil {
		return fmt.Errorf("ListenUart %s: %w", port, err)
	}

	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("ListenUart %s recv: %w", port, err)
		}
		if chunk.Error != nil && chunk.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
			return fmt.Errorf("ListenUart %s error: %s", port, chunk.Error.Message)
		}
		if len(chunk.Data) > 0 {
			onChunk(chunk.Data)
		}
	}
}

// ListenAsync starts an asynchronous UART stream, returning a channel of byte
// slices.  The channel is closed when the stream ends or ctx is cancelled.
func (m *UartManager) ListenAsync(ctx context.Context, port string, maxChunkSize int) (<-chan []byte, error) {
	req := &uartv26.UartReadRequest{
		Port:         port,
		MaxChunkSize: int32(maxChunkSize),
	}
	stream, err := m.client.ListenUart(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ListenUart %s: %w", port, err)
	}

	ch := make(chan []byte, 32)
	go func() {
		defer close(ch)
		for {
			chunk, err := stream.Recv()
			if err != nil {
				return
			}
			if len(chunk.GetData()) > 0 {
				select {
				case ch <- chunk.Data:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// ─── Enum conversion helpers ──────────────────────────────────────────────────

func sdkParityToProto(p UartParity) uartv26.UartParity {
	switch p {
	case UartParityEven:
		return uartv26.UartParity_UART_PARITY_EVEN
	case UartParityOdd:
		return uartv26.UartParity_UART_PARITY_ODD
	default:
		return uartv26.UartParity_UART_PARITY_NONE
	}
}

func protoParityToSDK(p uartv26.UartParity) UartParity {
	switch p {
	case uartv26.UartParity_UART_PARITY_EVEN:
		return UartParityEven
	case uartv26.UartParity_UART_PARITY_ODD:
		return UartParityOdd
	default:
		return UartParityNone
	}
}

func sdkStopBitsToProto(s UartStopBits) uartv26.UartStopBits {
	if s == UartStopBits2 {
		return uartv26.UartStopBits_UART_STOPBITS_2
	}
	return uartv26.UartStopBits_UART_STOPBITS_1
}

func protoStopBitsToSDK(s uartv26.UartStopBits) UartStopBits {
	if s == uartv26.UartStopBits_UART_STOPBITS_2 {
		return UartStopBits2
	}
	return UartStopBits1
}

func sdkFlowControlToProto(f UartFlowControl) uartv26.UartFlowControl {
	switch f {
	case UartFlowHardware:
		return uartv26.UartFlowControl_UART_FLOW_RTSCTS
	case UartFlowSoftware:
		return uartv26.UartFlowControl_UART_FLOW_XONXOFF
	default:
		return uartv26.UartFlowControl_UART_FLOW_NONE
	}
}

func protoFlowControlToSDK(f uartv26.UartFlowControl) UartFlowControl {
	switch f {
	case uartv26.UartFlowControl_UART_FLOW_RTSCTS:
		return UartFlowHardware
	case uartv26.UartFlowControl_UART_FLOW_XONXOFF:
		return UartFlowSoftware
	default:
		return UartFlowNone
	}
}
