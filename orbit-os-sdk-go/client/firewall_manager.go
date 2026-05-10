package client

import (
	"context"
	"fmt"

	common "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	fwsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/firewall_service/v26"
)

// FirewallManager provides access to the FirewallService gRPC service.
type FirewallManager struct {
	client fwsvcv26.FirewallServiceClient
	ctx    context.Context
}

func NewFirewallManager(client fwsvcv26.FirewallServiceClient, ctx context.Context) *FirewallManager {
	return &FirewallManager{client: client, ctx: ctx}
}

// ── Zones ─────────────────────────────────────────────────────────────────────

// ListZones returns all configured firewall zones.
func (f *FirewallManager) ListZones() ([]*fwsvcv26.ZoneRequest, error) {
	resp, err := f.client.ListZones(f.ctx, &common.Empty{})
	if err != nil {
		return nil, fmt.Errorf("ListZones: %w", err)
	}
	if err := checkError(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetZones(), nil
}

// AddZone creates a new firewall zone.
func (f *FirewallManager) AddZone(
	name string,
	interfaces []string,
	inputPolicy fwsvcv26.ZonePolicy,
	outputPolicy fwsvcv26.ZonePolicy,
	masquerade bool,
) (bool, error) {
	req := &fwsvcv26.ZoneRequest{
		Name:         name,
		Interfaces:   interfaces,
		InputPolicy:  inputPolicy,
		OutputPolicy: outputPolicy,
		Masquerade:   masquerade,
	}
	resp, err := f.client.AddZone(f.ctx, req)
	if err != nil {
		return false, fmt.Errorf("AddZone: %w", err)
	}
	if err := checkError(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// RemoveZone deletes a zone by name.
func (f *FirewallManager) RemoveZone(name string) (bool, error) {
	resp, err := f.client.RemoveZone(f.ctx, &fwsvcv26.ZoneNameRequest{Name: name})
	if err != nil {
		return false, fmt.Errorf("RemoveZone: %w", err)
	}
	if err := checkError(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// ── Rules ─────────────────────────────────────────────────────────────────────

// ListRules returns all configured firewall rules.
func (f *FirewallManager) ListRules() ([]*fwsvcv26.FirewallRule, error) {
	resp, err := f.client.ListRules(f.ctx, &common.Empty{})
	if err != nil {
		return nil, fmt.Errorf("ListRules: %w", err)
	}
	if err := checkError(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetRules(), nil
}

// AddRule adds a traffic rule. Returns the assigned rule ID on success.
func (f *FirewallManager) AddRule(
	srcZone, dstZone string,
	protocol fwsvcv26.FirewallProtocol,
	srcIP string,
	destPort int32,
	action fwsvcv26.ZonePolicy,
	comment string,
) (bool, error) {
	req := &fwsvcv26.FirewallRuleRequest{
		SrcZone:  srcZone,
		DstZone:  dstZone,
		Protocol: protocol,
		SrcIp:    srcIP,
		DestPort: destPort,
		Action:   action,
		Comment:  comment,
	}
	resp, err := f.client.AddRule(f.ctx, req)
	if err != nil {
		return false, fmt.Errorf("AddRule: %w", err)
	}
	if err := checkError(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// RemoveRule removes a rule by its ID.
func (f *FirewallManager) RemoveRule(id string) (bool, error) {
	resp, err := f.client.RemoveRule(f.ctx, &fwsvcv26.FirewallRuleIdRequest{Id: id})
	if err != nil {
		return false, fmt.Errorf("RemoveRule: %w", err)
	}
	if err := checkError(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// FlushRules removes all traffic rules (zones remain).
func (f *FirewallManager) FlushRules() (bool, error) {
	resp, err := f.client.FlushRules(f.ctx, &common.Empty{})
	if err != nil {
		return false, fmt.Errorf("FlushRules: %w", err)
	}
	if err := checkError(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// ── helper ────────────────────────────────────────────────────────────────────

func checkError(e *common.ErrorInfo) error {
	if e == nil || e.GetCode() == common.ErrorCode_ERROR_CODE_NONE {
		return nil
	}
	return fmt.Errorf("server error %v: %s", e.GetCode(), e.GetMessage())
}
