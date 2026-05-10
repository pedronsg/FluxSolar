package client

import (
	"context"
	"fmt"

	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	wifisvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/wifi_service/v26"
)

// WiFiManager is the SDK client for WiFiService gRPC.
type WiFiManager struct {
	client wifisvcv26.WiFiServiceClient
	ctx    context.Context
}

func NewWiFiManager(client wifisvcv26.WiFiServiceClient, ctx context.Context) *WiFiManager {
	return &WiFiManager{client: client, ctx: ctx}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func wifiErrCheck(info *common.ErrorInfo) error {
	if info != nil && info.GetCode() != common.ErrorCode_ERROR_CODE_NONE {
		return fmt.Errorf("server error [%v]: %s", info.GetCode(), info.GetMessage())
	}
	return nil
}

// ── Interface info ────────────────────────────────────────────────────────────

// ListInterfaces returns all Wi‑Fi interfaces.
func (w *WiFiManager) ListInterfaces() ([]*wifisvcv26.WiFiLinkProperties, error) {
	resp, err := w.client.ListWiFiInterfaces(w.ctx, &common.Empty{})
	if err != nil {
		return nil, fmt.Errorf("ListWiFiInterfaces: %w", err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetInterfaces(), nil
}

// GetLinkProperties returns link properties for a Wi‑Fi interface.
func (w *WiFiManager) GetLinkProperties(ifname string) (*wifisvcv26.WiFiLinkProperties, error) {
	resp, err := w.client.GetWiFiLinkProperties(w.ctx, &wifisvcv26.WiFiInterfaceRequest{
		InterfaceName: ifname,
	})
	if err != nil {
		return nil, fmt.Errorf("GetWiFiLinkProperties(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetProperties(), nil
}

// IsConnected reports whether the Wi‑Fi interface is connected.
func (w *WiFiManager) IsConnected(ifname string) (bool, error) {
	resp, err := w.client.IsWiFiConnected(w.ctx, &wifisvcv26.WiFiInterfaceRequest{
		InterfaceName: ifname,
	})
	if err != nil {
		return false, fmt.Errorf("IsWiFiConnected(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// ── Mode ──────────────────────────────────────────────────────────────────────

// GetMode returns the current interface mode (CLIENT, AP, UNKNOWN).
func (w *WiFiManager) GetMode(ifname string) (wifisvcv26.WiFiMode, error) {
	resp, err := w.client.GetWiFiMode(w.ctx, &wifisvcv26.WiFiInterfaceRequest{
		InterfaceName: ifname,
	})
	if err != nil {
		return wifisvcv26.WiFiMode_WIFI_MODE_UNKNOWN, fmt.Errorf("GetWiFiMode(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return wifisvcv26.WiFiMode_WIFI_MODE_UNKNOWN, err
	}
	return resp.GetMode(), nil
}

// SetModeClient switches the interface to client mode (uses stored config).
func (w *WiFiManager) SetModeClient(ifname string) (bool, error) {
	resp, err := w.client.SetWiFiMode(w.ctx, &wifisvcv26.SetWiFiModeRequest{
		InterfaceName: ifname,
		Mode:          wifisvcv26.WiFiMode_WIFI_MODE_CLIENT,
	})
	if err != nil {
		return false, fmt.Errorf("SetWiFiMode CLIENT(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// ── Client ────────────────────────────────────────────────────────────────────

// SetClientConfig stores Wi‑Fi client settings and connects.
// If dhcpEnable=true, ipv4Address/ipv4Gateway/ipv4Dns are ignored.
func (w *WiFiManager) SetClientConfig(ifname, ssid, password, security string, dhcpEnable bool, ipv4Address, ipv4Gateway string, ipv4Dns []string) (bool, error) {
	resp, err := w.client.SetClientConfig(w.ctx, &wifisvcv26.ClientConfig{
		InterfaceName: ifname,
		Ssid:          ssid,
		Password:      password,
		Security:      security,
		DhcpEnable:    dhcpEnable,
		Ipv4Address:   ipv4Address,
		Ipv4Gateway:   ipv4Gateway,
		Ipv4Dns:       ipv4Dns,
	})
	if err != nil {
		return false, fmt.Errorf("SetClientConfig(%s ssid=%s): %w", ifname, ssid, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// GetClientProperties returns IP properties for the current client connection.
func (w *WiFiManager) GetClientProperties(ifname string) (*wifisvcv26.ClientProperties, error) {
	resp, err := w.client.GetClientProperties(w.ctx, &wifisvcv26.WiFiInterfaceRequest{
		InterfaceName: ifname,
	})
	if err != nil {
		return nil, fmt.Errorf("GetClientProperties(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetProperties(), nil
}

// Connect connects using the previously stored config (via SetClientConfig).
func (w *WiFiManager) Connect(ifname string) (bool, error) {
	resp, err := w.client.Connect(w.ctx, &wifisvcv26.WiFiInterfaceRequest{
		InterfaceName: ifname,
	})
	if err != nil {
		return false, fmt.Errorf("Connect(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// Disconnect disconnects from the Wi‑Fi network.
func (w *WiFiManager) Disconnect(ifname string) (bool, error) {
	resp, err := w.client.Disconnect(w.ctx, &wifisvcv26.WiFiInterfaceRequest{
		InterfaceName: ifname,
	})
	if err != nil {
		return false, fmt.Errorf("Disconnect(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// ── Access Point ──────────────────────────────────────────────────────────────

// StartHotspot configura e inicia o Access Point.
// band: "2.4GHz" or "5GHz"; channel: 0 = auto.
func (w *WiFiManager) StartHotspot(ifname, ssid, password, band string, channel int32) (bool, error) {
	resp, err := w.client.SetAPConfig(w.ctx, &wifisvcv26.APConfig{
		InterfaceName: ifname,
		Ssid:          ssid,
		Password:      password,
		Band:          band,
		Channel:       channel,
	})
	if err != nil {
		return false, fmt.Errorf("StartHotspot(%s ssid=%s): %w", ifname, ssid, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// StopHotspot stops the access point.
func (w *WiFiManager) StopHotspot(ifname string) (bool, error) {
	resp, err := w.client.StopAP(w.ctx, &wifisvcv26.WiFiInterfaceRequest{
		InterfaceName: ifname,
	})
	if err != nil {
		return false, fmt.Errorf("StopHotspot(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// GetAPProperties returns access point properties.
func (w *WiFiManager) GetAPProperties(ifname string) (*wifisvcv26.APProperties, error) {
	resp, err := w.client.GetAPProperties(w.ctx, &wifisvcv26.WiFiInterfaceRequest{
		InterfaceName: ifname,
	})
	if err != nil {
		return nil, fmt.Errorf("GetAPProperties(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetProperties(), nil
}

// ── Scan ──────────────────────────────────────────────────────────────────────

// Scan scans for available Wi‑Fi networks.
// If forceRescan=true, forces a new hardware scan (slower, ~3s).
func (w *WiFiManager) Scan(ifname string, forceRescan bool) ([]*wifisvcv26.ScannedNetwork, error) {
	resp, err := w.client.ScanWiFi(w.ctx, &wifisvcv26.ScanWiFiRequest{
		InterfaceName: ifname,
		ForceRescan:   forceRescan,
	})
	if err != nil {
		return nil, fmt.Errorf("ScanWiFi(%s): %w", ifname, err)
	}
	if err := wifiErrCheck(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetNetworks(), nil
}
