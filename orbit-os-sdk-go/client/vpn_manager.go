package client

import (
	"context"
	"fmt"
	"io"

	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	vpnv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/vpn_service/v26"
)

// VPNManager is the SDK client for VPNService gRPC.
type VPNManager struct {
	client vpnv26.VPNServiceClient
	ctx    context.Context
}

func NewVPNManager(client vpnv26.VPNServiceClient, ctx context.Context) *VPNManager {
	return &VPNManager{client: client, ctx: ctx}
}

func vpnErrCheck(info *common.ErrorInfo) error {
	if info != nil && info.GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return fmt.Errorf("server error [%v]: %s", info.GetCode(), info.GetMessage())
	}
	return nil
}

// ── Capabilities ──────────────────────────────────────────────────────────────

// GetCapabilities reports which VPN providers are available on the device.
func (v *VPNManager) GetCapabilities() (*vpnv26.VpnCapabilities, error) {
	resp, err := v.client.GetCapabilities(v.ctx, &common.Empty{})
	if err != nil {
		return nil, fmt.Errorf("GetCapabilities: %w", err)
	}
	return resp.GetCapabilities(), nil
}

// ── Profiles ──────────────────────────────────────────────────────────────────

// ListProfiles returns all saved VPN profiles (config_data is not included).
func (v *VPNManager) ListProfiles() ([]*vpnv26.VpnProfile, error) {
	resp, err := v.client.ListProfiles(v.ctx, &common.Empty{})
	if err != nil {
		return nil, fmt.Errorf("ListProfiles: %w", err)
	}
	if err := vpnErrCheck(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetProfiles(), nil
}

// ApplyProfile creates or updates a VPN profile and optionally connects to it.
// profile.ProfileId may be empty — the server assigns one and returns it.
func (v *VPNManager) ApplyProfile(profile *vpnv26.VpnProfile, connectAfterApply bool) (string, error) {
	resp, err := v.client.ApplyProfile(v.ctx, &vpnv26.ApplyProfileRequest{
		Profile:           profile,
		ConnectAfterApply: connectAfterApply,
	})
	if err != nil {
		return "", fmt.Errorf("ApplyProfile: %w", err)
	}
	if err := vpnErrCheck(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetActiveProfileId(), nil
}

// ApplyWireGuard is a convenience wrapper for WireGuard profiles.
// configData is the raw .conf file content.
func (v *VPNManager) ApplyWireGuard(displayName string, configData []byte, autoConnect bool) (string, error) {
	return v.ApplyProfile(&vpnv26.VpnProfile{
		DisplayName: displayName,
		Provider:    vpnv26.VpnProvider_VPN_PROVIDER_WIREGUARD,
		ConfigData:  configData,
		AutoConnect: autoConnect,
	}, false)
}

// ApplyOpenVPN is a convenience wrapper for OpenVPN profiles.
// configData is the raw .ovpn file content.
func (v *VPNManager) ApplyOpenVPN(displayName string, configData []byte, autoConnect bool) (string, error) {
	return v.ApplyProfile(&vpnv26.VpnProfile{
		DisplayName: displayName,
		Provider:    vpnv26.VpnProvider_VPN_PROVIDER_OPENVPN,
		ConfigData:  configData,
		AutoConnect: autoConnect,
	}, false)
}

// RemoveProfile deletes a VPN profile by ID (disconnects first if active).
func (v *VPNManager) RemoveProfile(profileID string) (bool, error) {
	resp, err := v.client.RemoveProfile(v.ctx, &vpnv26.VpnProfileRequest{ProfileId: profileID})
	if err != nil {
		return false, fmt.Errorf("RemoveProfile(%s): %w", profileID, err)
	}
	if err := vpnErrCheck(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// ── Connection ────────────────────────────────────────────────────────────────

// Connect activates a VPN profile by ID and returns the session ID.
// The tunnel comes up asynchronously — use WatchEvents or GetStatus to track.
func (v *VPNManager) Connect(profileID string) (string, error) {
	resp, err := v.client.Connect(v.ctx, &vpnv26.VpnProfileRequest{ProfileId: profileID})
	if err != nil {
		return "", fmt.Errorf("Connect(%s): %w", profileID, err)
	}
	if err := vpnErrCheck(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetSessionId(), nil
}

// Disconnect tears down the active tunnel for the given profile.
// Pass an empty profileID to disconnect whatever is currently active.
func (v *VPNManager) Disconnect(profileID string) (bool, error) {
	resp, err := v.client.Disconnect(v.ctx, &vpnv26.VpnProfileRequest{ProfileId: profileID})
	if err != nil {
		return false, fmt.Errorf("Disconnect(%s): %w", profileID, err)
	}
	if err := vpnErrCheck(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// ── Status ────────────────────────────────────────────────────────────────────

// GetStatus returns the current active session and provider-specific details.
// Session is nil when no tunnel is active.
func (v *VPNManager) GetStatus() (*vpnv26.Session, string, error) {
	resp, err := v.client.GetStatus(v.ctx, &common.Empty{})
	if err != nil {
		return nil, "", fmt.Errorf("GetStatus: %w", err)
	}
	if err := vpnErrCheck(resp.GetError()); err != nil {
		return nil, "", err
	}
	return resp.GetSession(), resp.GetProviderDetails(), nil
}

// ListSessions returns all active sessions (at most one in the current implementation).
func (v *VPNManager) ListSessions() ([]*vpnv26.Session, error) {
	resp, err := v.client.ListSessions(v.ctx, &common.Empty{})
	if err != nil {
		return nil, fmt.Errorf("ListSessions: %w", err)
	}
	if err := vpnErrCheck(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetSessions(), nil
}

// IsConnected reports whether there is an active tunnel in state UP.
func (v *VPNManager) IsConnected() (bool, error) {
	sess, _, err := v.GetStatus()
	if err != nil {
		return false, err
	}
	return sess != nil && sess.GetState() == vpnv26.TunnelState_TUNNEL_STATE_UP, nil
}

// ── Events ────────────────────────────────────────────────────────────────────

// WatchEvents opens a streaming subscription to VPN tunnel events.
// handler is called for each event; the stream runs until ctx is cancelled
// or the server closes the connection.
// Pass an empty profileID to receive events from all profiles.
func (v *VPNManager) WatchEvents(profileID string, handler func(*vpnv26.VPNEvent)) error {
	stream, err := v.client.WatchEvents(v.ctx, &vpnv26.WatchEventsRequest{ProfileId: profileID})
	if err != nil {
		return fmt.Errorf("WatchEvents: %w", err)
	}
	for {
		evt, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("WatchEvents recv: %w", err)
		}
		handler(evt)
	}
}
