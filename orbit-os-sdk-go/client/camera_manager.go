package client

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	camv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/camera_service/v26"
)

// CameraManager is the SDK client for CameraService (capture, stream, lock, device info).
type CameraManager struct {
	client camv26.CameraServiceClient
}

// NewCameraManager constructs a CameraManager.
func NewCameraManager(client camv26.CameraServiceClient) *CameraManager {
	return &CameraManager{client: client}
}

// CaptureImageResult is one still frame returned by CaptureImage (typically JPEG in ImageData).
type CaptureImageResult struct {
	ImageData []byte
	Format    string
	Timestamp int64
}

// CaptureImage captures a single image from the V4L device (e.g. "/dev/video0").
// Gravity requires LockCamera(deviceID, clientID) for the same device before CaptureImage; pair with UnlockCamera when done.
func (m *CameraManager) CaptureImage(ctx context.Context, deviceID string, width, height int32, format string) (*CaptureImageResult, error) {
	resp, err := m.client.CaptureImage(ctx, &camv26.CaptureImageRequest{
		DeviceId: deviceID,
		Width:    width,
		Height:   height,
		Format:   format,
	})
	if err != nil {
		return nil, fmt.Errorf("CaptureImage: %w", err)
	}
	if err := rpcError(resp.GetError()); err != nil {
		return nil, err
	}
	if !resp.GetSuccess() {
		return nil, fmt.Errorf("CaptureImage: server reported failure")
	}
	return &CaptureImageResult{
		ImageData: resp.GetImageData(),
		Format:    resp.GetFormat(),
		Timestamp: resp.GetTimestamp(),
	}, nil
}

// StreamFrames opens a server stream of frames at the requested FPS and resolution.
// The caller must read from the returned stream until io.EOF or error.
func (m *CameraManager) StreamFrames(ctx context.Context, req *camv26.StreamFramesRequest) (grpc.ServerStreamingClient[camv26.Frame], error) {
	stream, err := m.client.StreamFrames(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("StreamFrames: %w", err)
	}
	return stream, nil
}

// LockCamera acquires an exclusive lock on the camera for this client ID.
func (m *CameraManager) LockCamera(ctx context.Context, deviceID, clientID string) error {
	resp, err := m.client.LockCamera(ctx, &camv26.LockRequest{
		DeviceId: deviceID,
		ClientId: clientID,
	})
	if err != nil {
		return fmt.Errorf("LockCamera: %w", err)
	}
	if err := rpcError(resp.GetError()); err != nil {
		return err
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("LockCamera: server reported failure")
	}
	return nil
}

// UnlockCamera releases the lock obtained with LockCamera.
func (m *CameraManager) UnlockCamera(ctx context.Context, deviceID, clientID string) error {
	resp, err := m.client.UnlockCamera(ctx, &camv26.UnlockRequest{
		DeviceId: deviceID,
		ClientId: clientID,
	})
	if err != nil {
		return fmt.Errorf("UnlockCamera: %w", err)
	}
	if err := rpcError(resp.GetError()); err != nil {
		return err
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("UnlockCamera: server reported failure")
	}
	return nil
}

// CameraDeviceInfo describes a V4L camera node (formats, resolutions, driver).
type CameraDeviceInfo struct {
	DeviceID         string
	Driver           string
	Card             string
	SupportedFormats []string
	Resolutions      []string
}

// ListDevices returns all V4L2 video nodes available on the Orbit device.
// Each entry is populated with driver, card and format info via GetDeviceInfo.
// Nodes that fail GetDeviceInfo are still returned with only the DeviceID set.
func (m *CameraManager) ListDevices(ctx context.Context) ([]*CameraDeviceInfo, error) {
	resp, err := m.client.ListDevices(ctx, &camv26.ListDevicesRequest{})
	if err != nil {
		return nil, fmt.Errorf("ListDevices: %w", err)
	}
	out := make([]*CameraDeviceInfo, 0, len(resp.GetDevices()))
	for _, d := range resp.GetDevices() {
		out = append(out, &CameraDeviceInfo{
			DeviceID:         d.GetDeviceId(),
			Driver:           d.GetDriver(),
			Card:             d.GetCard(),
			SupportedFormats: append([]string(nil), d.GetSupportedFormats()...),
			Resolutions:      append([]string(nil), d.GetResolutions()...),
		})
	}
	return out, nil
}

// GetDeviceInfo queries metadata for a device path (e.g. "/dev/video0").
func (m *CameraManager) GetDeviceInfo(ctx context.Context, deviceID string) (*CameraDeviceInfo, error) {
	resp, err := m.client.GetDeviceInfo(ctx, &camv26.DeviceInfoRequest{DeviceId: deviceID})
	if err != nil {
		return nil, fmt.Errorf("GetDeviceInfo: %w", err)
	}
	return &CameraDeviceInfo{
		DeviceID:         resp.GetDeviceId(),
		Driver:           resp.GetDriver(),
		Card:             resp.GetCard(),
		SupportedFormats: append([]string(nil), resp.GetSupportedFormats()...),
		Resolutions:      append([]string(nil), resp.GetResolutions()...),
	}, nil
}
