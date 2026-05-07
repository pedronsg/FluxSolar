package client

import (
	"context"
	"fmt"

	common "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	pwmv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/pwm_service/v26"
)

// PwmManager is the SDK client for PwmService.
type PwmManager struct {
	client pwmv26.PwmServiceClient
	ctx    context.Context
}

// NewPwmManager constructs a PwmManager.
func NewPwmManager(client pwmv26.PwmServiceClient, ctx context.Context) *PwmManager {
	return &PwmManager{client: client, ctx: ctx}
}

// PwmChannel identifies a hardware PWM channel.
type PwmChannel struct {
	Channel uint32
	Name    string
}

func toProtoPwmChannel(p *PwmChannel) *pwmv26.PwmChannel {
	if p == nil {
		return nil
	}
	return &pwmv26.PwmChannel{Channel: p.Channel, Name: p.Name}
}

func pwmChannelFromProto(p *pwmv26.PwmChannel) *PwmChannel {
	if p == nil {
		return nil
	}
	return &PwmChannel{Channel: p.Channel, Name: p.Name}
}

// PwmProperties is the current state of a PWM channel.
type PwmProperties struct {
	Channel     *PwmChannel
	Enabled     bool
	DutyCycle   float64 // 0.0–1.0
	FrequencyHz float64
}

// ListChannels returns all PWM channels available on the system.
func (m *PwmManager) ListChannels() ([]*PwmChannel, error) {
	resp, err := m.client.ListPwmChannels(m.ctx, &common.Empty{})
	if err != nil {
		return nil, fmt.Errorf("ListPwmChannels: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("ListPwmChannels: %s", resp.GetError().GetMessage())
	}
	var out []*PwmChannel
	for _, ch := range resp.GetChannels() {
		out = append(out, pwmChannelFromProto(ch))
	}
	return out, nil
}

// GetProperties reads the current PWM configuration for a channel.
func (m *PwmManager) GetProperties(ch *PwmChannel) (*PwmProperties, error) {
	resp, err := m.client.GetPwmProperties(m.ctx, &pwmv26.PwmChannelRequest{
		Channel: toProtoPwmChannel(ch),
	})
	if err != nil {
		return nil, fmt.Errorf("GetPwmProperties: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("GetPwmProperties: %s", resp.GetError().GetMessage())
	}
	p := resp.GetProperties()
	if p == nil {
		return nil, fmt.Errorf("GetPwmProperties: empty properties")
	}
	return &PwmProperties{
		Channel:     pwmChannelFromProto(p.GetChannel()),
		Enabled:     p.GetEnabled(),
		DutyCycle:   p.GetDutyCycle(),
		FrequencyHz: p.GetFrequencyHz(),
	}, nil
}

// SetPwm configures duty cycle (0.0–1.0) and frequency (Hz) and starts output.
func (m *PwmManager) SetPwm(ch *PwmChannel, dutyCycle float64, frequencyHz float64) error {
	resp, err := m.client.SetPwm(m.ctx, &pwmv26.SetPwmRequest{
		Channel:     toProtoPwmChannel(ch),
		DutyCycle:   dutyCycle,
		FrequencyHz: frequencyHz,
	})
	if err != nil {
		return fmt.Errorf("SetPwm: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return fmt.Errorf("SetPwm: %s", resp.GetError().GetMessage())
	}
	if !resp.GetValue() {
		return fmt.Errorf("SetPwm: server returned false")
	}
	return nil
}

// StopPwm disables output on the channel.
func (m *PwmManager) StopPwm(ch *PwmChannel) error {
	resp, err := m.client.StopPwm(m.ctx, &pwmv26.PwmChannelRequest{
		Channel: toProtoPwmChannel(ch),
	})
	if err != nil {
		return fmt.Errorf("StopPwm: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return fmt.Errorf("StopPwm: %s", resp.GetError().GetMessage())
	}
	if !resp.GetValue() {
		return fmt.Errorf("StopPwm: server returned false")
	}
	return nil
}
