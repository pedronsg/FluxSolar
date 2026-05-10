package client

import (
	common "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	ethsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/ethernet_service/v26"
	"context"
	"fmt"
)

type EthernetManager struct {
	client ethsvcv26.EthernetServiceClient
	ctx    context.Context
}

func NewEthernetManager(client ethsvcv26.EthernetServiceClient, ctx context.Context) *EthernetManager {
	return &EthernetManager{client: client, ctx: ctx}
}

// ListEthernetInterfaces returns all Ethernet interfaces.
func (e *EthernetManager) ListEthernetInterfaces() ([]*ethsvcv26.EthernetLinkProperties, error) {
	resp, err := e.client.ListEthernetInterfaces(e.ctx, &common.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to list ethernet interfaces: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("error from server: %s (code: %v)", resp.GetError().GetMessage(), resp.GetError().GetCode())
	}
	return resp.GetInterfaces(), nil
}

// IsEthernetConnected reports whether the interface has link.
func (e *EthernetManager) IsEthernetConnected(interfaceName string) (bool, error) {
	req := &ethsvcv26.InterfaceRequest{
		InterfaceName: interfaceName,
	}
	resp, err := e.client.IsEthernetConnected(e.ctx, req)
	if err != nil {
		return false, fmt.Errorf("failed to check ethernet connection: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return false, fmt.Errorf("error from server: %s (code: %v)", resp.GetError().GetMessage(), resp.GetError().GetCode())
	}
	return resp.GetValue(), nil
}

// GetEthernetLinkProperties returns link properties for an interface.
func (e *EthernetManager) GetEthernetLinkProperties(interfaceName string) (*ethsvcv26.EthernetLinkProperties, error) {
	req := &ethsvcv26.InterfaceRequest{
		InterfaceName: interfaceName,
	}
	resp, err := e.client.GetEthernetLinkProperties(e.ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get ethernet link properties: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return nil, fmt.Errorf("error from server: %s (code: %v)", resp.GetError().GetMessage(), resp.GetError().GetCode())
	}
	return resp.GetProperties(), nil
}

// SetEthernetConfig applies static IP or DHCP settings.
func (e *EthernetManager) SetEthernetConfig(interfaceName string, enable bool, dhcpEnable bool, ipv4Address, ipv4Gateway string, ipv4Dns []string) (bool, error) {
	req := &ethsvcv26.EthernetConfig{
		InterfaceName: interfaceName,
		DhcpEnable:    dhcpEnable,
		Ipv4Address:   ipv4Address,
		Ipv4Gateway:   ipv4Gateway,
		Ipv4Dns:       ipv4Dns,
	}
	resp, err := e.client.SetEthernetConfig(e.ctx, req)
	if err != nil {
		return false, fmt.Errorf("failed to set ethernet config: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return false, fmt.Errorf("error from server: %s (code: %v)", resp.GetError().GetMessage(), resp.GetError().GetCode())
	}
	return resp.GetValue(), nil
}

// EnableEthernet brings the interface up.
func (e *EthernetManager) EnableEthernet(interfaceName string) (bool, error) {
	req := &ethsvcv26.InterfaceRequest{
		InterfaceName: interfaceName,
	}
	resp, err := e.client.EnableEthernet(e.ctx, req)
	if err != nil {
		return false, fmt.Errorf("failed to enable ethernet: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return false, fmt.Errorf("error from server: %s (code: %v)", resp.GetError().GetMessage(), resp.GetError().GetCode())
	}
	return resp.GetValue(), nil
}

// DisableEthernet brings the interface down.
func (e *EthernetManager) DisableEthernet(interfaceName string) (bool, error) {
	req := &ethsvcv26.InterfaceRequest{
		InterfaceName: interfaceName,
	}
	resp, err := e.client.DisableEthernet(e.ctx, req)
	if err != nil {
		return false, fmt.Errorf("failed to disable ethernet: %w", err)
	}
	if resp.GetError() != nil && resp.GetError().GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return false, fmt.Errorf("error from server: %s (code: %v)", resp.GetError().GetMessage(), resp.GetError().GetCode())
	}
	return resp.GetValue(), nil
}
