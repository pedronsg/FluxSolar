package client

import (
	"context"
	"fmt"
	"io"

	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	btsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/bluetooth_service/v26"
)

// BluetoothManager wraps the BluetoothService gRPC client.
type BluetoothManager struct {
	client btsvcv26.BluetoothServiceClient
}

func newBluetoothManager(client btsvcv26.BluetoothServiceClient) *BluetoothManager {
	return &BluetoothManager{client: client}
}

// ─── Adapter ──────────────────────────────────────────────────────────────────

// AdapterInfo holds info about the local BT adapter (SDK type).
type AdapterInfo struct {
	Address      string
	Name         string
	Powered      bool
	Discoverable bool
	Discovering  bool
}

// GetAdapterInfo returns information about the local Bluetooth adapter.
func (m *BluetoothManager) GetAdapterInfo(ctx context.Context) (*AdapterInfo, error) {
	resp, err := m.client.GetAdapterInfo(ctx, &common.Empty{})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil {
		return nil, fmt.Errorf("%s", resp.GetError().GetMessage())
	}
	info := resp.GetInfo()
	return &AdapterInfo{
		Address:      info.GetAddress(),
		Name:         info.GetName(),
		Powered:      info.GetState() == btsvcv26.BluetoothState_BT_STATE_ON,
		Discoverable: info.GetDiscoverable(),
		Discovering:  info.GetDiscovering(),
	}, nil
}

// EnableBluetooth powers on the Bluetooth adapter.
func (m *BluetoothManager) EnableBluetooth(ctx context.Context) (bool, error) {
	resp, err := m.client.EnableBluetooth(ctx, &common.Empty{})
	if err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// DisableBluetooth powers off the Bluetooth adapter.
func (m *BluetoothManager) DisableBluetooth(ctx context.Context) (bool, error) {
	resp, err := m.client.DisableBluetooth(ctx, &common.Empty{})
	if err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// GetLocalName returns the adapter's advertising name.
func (m *BluetoothManager) GetLocalName(ctx context.Context) (string, error) {
	resp, err := m.client.GetLocalName(ctx, &common.Empty{})
	if err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

// SetLocalName sets the adapter's advertising name.
func (m *BluetoothManager) SetLocalName(ctx context.Context, name string) (bool, error) {
	resp, err := m.client.SetLocalName(ctx, &common.StringRequest{Value: name})
	if err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// SetDiscoverable makes the adapter discoverable for durationSec seconds (0 = indefinite).
func (m *BluetoothManager) SetDiscoverable(ctx context.Context, enable bool, durationSec int32) (bool, error) {
	resp, err := m.client.SetDiscoverable(ctx, &btsvcv26.SetDiscoverableRequest{
		Discoverable: enable,
		TimeoutSec:   durationSec,
	})
	if err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// ─── Classic Scan ─────────────────────────────────────────────────────────────

// BtDevice holds information about a remote Bluetooth device (SDK type).
type BtDevice struct {
	Address   string
	Name      string
	Type      string // "classic", "le", "dual", "unknown"
	Bonded    bool
	RSSI      int32
}

func protoDevToSDK(d *btsvcv26.BluetoothDevice) BtDevice {
	if d == nil {
		return BtDevice{}
	}
	devType := "unknown"
	switch d.GetType() {
	case btsvcv26.BluetoothDeviceType_BT_DEVICE_CLASSIC:
		devType = "classic"
	case btsvcv26.BluetoothDeviceType_BT_DEVICE_LE:
		devType = "le"
	case btsvcv26.BluetoothDeviceType_BT_DEVICE_DUAL:
		devType = "dual"
	}
	return BtDevice{
		Address: d.GetAddress(),
		Name:    d.GetName(),
		Type:    devType,
		Bonded:  d.GetBondState() == btsvcv26.BondState_BOND_BONDED,
		RSSI:    d.GetRssi(),
	}
}

// ScanClassic starts a classic Bluetooth scan and calls onResult for each found device.
// Blocks until the context is cancelled or the scan times out (~30s).
func (m *BluetoothManager) ScanClassic(ctx context.Context, onResult func(BtDevice)) error {
	stream, err := m.client.StartScan(ctx, &common.Empty{})
	if err != nil {
		return err
	}
	for {
		result, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		onResult(protoDevToSDK(result.GetDevice()))
	}
}

// GetBondedDevices returns all paired/bonded devices.
func (m *BluetoothManager) GetBondedDevices(ctx context.Context) ([]BtDevice, error) {
	resp, err := m.client.GetBondedDevices(ctx, &common.Empty{})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil {
		return nil, fmt.Errorf("%s", resp.GetError().GetMessage())
	}
	var devices []BtDevice
	for _, d := range resp.GetDevices() {
		devices = append(devices, protoDevToSDK(d))
	}
	return devices, nil
}

// BondEvent represents a pairing state event (SDK type).
type BondEvent struct {
	State string // "bonding", "bonded", "failed"
	Pin   string
	Error string
}

// BondDevice initiates pairing and calls onEvent for each bond state change.
// Blocks until bonding completes, fails, or context is cancelled.
func (m *BluetoothManager) BondDevice(ctx context.Context, address string, onEvent func(BondEvent)) error {
	stream, err := m.client.BondDevice(ctx, &btsvcv26.BtDeviceRequest{Address: address})
	if err != nil {
		return err
	}
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		stateStr := "bonding"
		switch ev.GetState() {
		case btsvcv26.BondState_BOND_BONDED:
			stateStr = "bonded"
		case btsvcv26.BondState_BOND_NONE:
			stateStr = "failed"
		}
		errMsg := ""
		if ev.GetError() != nil {
			errMsg = ev.GetError().GetMessage()
		}
		onEvent(BondEvent{State: stateStr, Pin: ev.GetPin(), Error: errMsg})
	}
}

// RemoveBond removes the bond with a device.
func (m *BluetoothManager) RemoveBond(ctx context.Context, address string) (bool, error) {
	resp, err := m.client.RemoveBond(ctx, &btsvcv26.BtDeviceRequest{Address: address})
	if err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// ConnectDevice connects to a bonded device.
func (m *BluetoothManager) ConnectDevice(ctx context.Context, address string) (bool, error) {
	resp, err := m.client.ConnectDevice(ctx, &btsvcv26.BtDeviceRequest{Address: address})
	if err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// DisconnectDevice disconnects a connected device.
func (m *BluetoothManager) DisconnectDevice(ctx context.Context, address string) (bool, error) {
	resp, err := m.client.DisconnectDevice(ctx, &btsvcv26.BtDeviceRequest{Address: address})
	if err != nil {
		return false, err
	}
	return resp.GetValue(), nil
}

// GetConnectionState returns whether a device is connected.
func (m *BluetoothManager) GetConnectionState(ctx context.Context, address string) (string, error) {
	resp, err := m.client.GetConnectionState(ctx, &btsvcv26.BtDeviceRequest{Address: address})
	if err != nil {
		return "", err
	}
	if resp.GetError() != nil {
		return "", fmt.Errorf("%s", resp.GetError().GetMessage())
	}
	switch resp.GetState() {
	case btsvcv26.BtConnectionState_BT_CONN_CONNECTED:
		return "connected", nil
	case btsvcv26.BtConnectionState_BT_CONN_CONNECTING:
		return "connecting", nil
	case btsvcv26.BtConnectionState_BT_CONN_DISCONNECTING:
		return "disconnecting", nil
	default:
		return "disconnected", nil
	}
}

// ─── BLE Scan ─────────────────────────────────────────────────────────────────

// BLEScanResult holds information about a discovered BLE device (SDK type).
type BLEScanResult struct {
	Address      string
	Name         string
	RSSI         int32
	Connectable  bool
	ServiceUUIDs []string
}

// BLEScanFilter filters BLE scan results.
type BLEScanFilter struct {
	NamePrefix  string
	Address     string
	ServiceUUID string
}

// ScanBLE starts a BLE scan and calls onResult for each discovered device.
// Blocks until context is cancelled or scan times out (~30s).
func (m *BluetoothManager) ScanBLE(ctx context.Context, filters []BLEScanFilter, onResult func(BLEScanResult)) error {
	var protoFilters []*btsvcv26.BleScanFilter
	for _, f := range filters {
		protoFilters = append(protoFilters, &btsvcv26.BleScanFilter{
			NamePrefix:  f.NamePrefix,
			Address:     f.Address,
			ServiceUuid: f.ServiceUUID,
		})
	}

	stream, err := m.client.StartBleScan(ctx, &btsvcv26.BleScanRequest{
		Filters: protoFilters,
	})
	if err != nil {
		return err
	}
	for {
		result, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		onResult(BLEScanResult{
			Address:      result.GetAddress(),
			Name:         result.GetName(),
			RSSI:         result.GetRssi(),
			Connectable:  result.GetConnectable(),
			ServiceUUIDs: result.GetServiceUuids(),
		})
	}
}
