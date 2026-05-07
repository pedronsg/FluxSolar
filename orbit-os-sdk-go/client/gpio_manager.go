package client

import (
	"context"
	"fmt"

	types "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	gpiov26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/gpio_service/v26"
)

// GpioLevel is the logical line level.
type GpioLevel int32

const (
	GPIO_LEVEL_LOW  GpioLevel = 0 // LOW
	GPIO_LEVEL_HIGH GpioLevel = 1 // HIGH
)

func (l GpioLevel) toProto() gpiov26.GpioLevel {
	return gpiov26.GpioLevel(l)
}

func gpioLevelFromProto(v gpiov26.GpioLevel) GpioLevel {
	return GpioLevel(v)
}

// GpioDirection is line direction.
type GpioDirection int32

const (
	GPIO_DIR_OUT GpioDirection = 0 // out
	GPIO_DIR_IN  GpioDirection = 1 // input
)

func (d GpioDirection) toProto() gpiov26.GpioDirection {
	return gpiov26.GpioDirection(d)
}

func gpioDirectionFromProto(v gpiov26.GpioDirection) GpioDirection {
	return GpioDirection(v)
}

// GpioManager is the SDK client for GpioService.
type GpioManager struct {
	client gpiov26.GpioServiceClient
	ctx    context.Context
}

// NewGpioManager constructs a GpioManager.
func NewGpioManager(client gpiov26.GpioServiceClient, ctx context.Context) *GpioManager {
	return &GpioManager{client: client, ctx: ctx}
}

// GpioPin identifies a GPIO line (chip index + offset).
type GpioPin struct {
	Name       string
	Number     int32 // line offset within the chip (e.g. 26 for GPIO26 on RPi4)
	ChipNumber int32 // chip index (e.g. 0 for /dev/gpiochip0)
}

// toProtoPin maps a GpioPin to the protobuf type.
func toProtoPin(p *GpioPin) *gpiov26.GpioPin {
	if p == nil {
		return nil
	}
	out := &gpiov26.GpioPin{
		Line:     p.Number,
		Gpiochip: p.ChipNumber,
	}
	if p.Name != "" {
		out.Name = p.Name
	}
	return out
}

// ─── GPIO ─────────────────────────────────────────────────────────────────────

// ListPins returns all GPIO lines available on the system.
func (m *GpioManager) ListPins() ([]*GpioPin, error) {
	resp, err := m.client.ListGPIOPins(m.ctx, &types.Void{})
	if err != nil {
		return nil, fmt.Errorf("ListPins: %w", err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("ListPins: %s", resp.Error.Message)
	}

	var pins []*GpioPin
	for _, p := range resp.Pins {
		pins = append(pins, &GpioPin{
			Name:       p.Name,
			Number:     p.Line,
			ChipNumber: p.Gpiochip,
		})
	}
	return pins, nil
}

// GetLevel reads the current level of a GPIO line.
func (m *GpioManager) GetLevel(pin *GpioPin) (GpioLevel, error) {
	resp, err := m.client.GetGPIOLevel(m.ctx, &gpiov26.GpioLevelRequest{
		Pin: toProtoPin(pin),
	})
	if err != nil {
		return GPIO_LEVEL_LOW, fmt.Errorf("GetLevel GPIO%d (chip%d): %w", pin.Number, pin.ChipNumber, err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return GPIO_LEVEL_LOW, fmt.Errorf("GetLevel GPIO%d (chip%d): %s", pin.Number, pin.ChipNumber, resp.Error.Message)
	}
	return gpioLevelFromProto(resp.Level), nil
}

// SetLevel drives a GPIO line.
func (m *GpioManager) SetLevel(pin *GpioPin, level GpioLevel) error {
	resp, err := m.client.SetGPIOLevel(m.ctx, &gpiov26.GpioLevelRequest{
		Pin:   toProtoPin(pin),
		Level: level.toProto(),
	})
	if err != nil {
		return fmt.Errorf("SetLevel GPIO%d (chip%d): %w", pin.Number, pin.ChipNumber, err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return fmt.Errorf("SetLevel GPIO%d (chip%d): %s", pin.Number, pin.ChipNumber, resp.Error.Message)
	}
	return nil
}

// GetDirection reads direction.
func (m *GpioManager) GetDirection(pin *GpioPin) (GpioDirection, error) {
	resp, err := m.client.GetGPIODirection(m.ctx, &gpiov26.GpioDirectionRequest{
		Pin: toProtoPin(pin),
	})
	if err != nil {
		return GPIO_DIR_IN, fmt.Errorf("GetDirection GPIO%d (chip%d): %w", pin.Number, pin.ChipNumber, err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return GPIO_DIR_IN, fmt.Errorf("GetDirection GPIO%d (chip%d): %s", pin.Number, pin.ChipNumber, resp.Error.Message)
	}
	return gpioDirectionFromProto(resp.Direction), nil
}

// SetDirection sets direction.
func (m *GpioManager) SetDirection(pin *GpioPin, dir GpioDirection) error {
	resp, err := m.client.SetGPIODirection(m.ctx, &gpiov26.GpioDirectionRequest{
		Pin:       toProtoPin(pin),
		Direction: dir.toProto(),
	})
	if err != nil {
		return fmt.Errorf("SetDirection GPIO%d (chip%d): %w", pin.Number, pin.ChipNumber, err)
	}
	if resp.Error != nil && resp.Error.Code != types.ErrorCode_ERROR_CODE_NONE {
		return fmt.Errorf("SetDirection GPIO%d (chip%d): %s", pin.Number, pin.ChipNumber, resp.Error.Message)
	}
	return nil
}
