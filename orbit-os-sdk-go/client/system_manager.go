package client

import (
	"context"
	"fmt"

	types "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	systemv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/system_service/v26"
)

type SystemManager struct {
	client systemv26.SystemServiceClient
	ctx    context.Context
}

func NewSystemManager(client systemv26.SystemServiceClient, ctx context.Context) *SystemManager {
	return &SystemManager{client: client, ctx: ctx}
}

// Each SystemService RPC is exposed as a direct method.
func rpcError(info *types.ErrorInfo) error {
	if info == nil || info.GetCode() == types.ErrorCode_ERROR_CODE_NONE {
		return nil
	}
	return fmt.Errorf("error from server: %s (code: %v)", info.GetMessage(), info.GetCode())
}



// GetApiVersion returns the System Service API version and revision as reported by the device.
func (s *SystemManager) GetApiVersion() (version int64, revision int64, err error) {
	resp, err := s.client.GetApiVersion(s.ctx, &types.Void{})
	if err != nil {
		return 0, 0, err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return 0, 0, err
	}
	return resp.GetVersion(), resp.GetRevision(), nil
}


// GetApiVersionInfo returns the System Service API version and revision as reported by the device.
func (s *SystemManager) GetApiVersionInfo() (version string, err error) {
	resp, err := s.client.GetApiVersionInfo(s.ctx, &types.Void{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}



func (s *SystemManager) GetDeviceName() (string, error) {
	resp, err := s.client.GetDeviceName(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetSocModel() (string, error) {
	resp, err := s.client.GetSocModel(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetSocVendor() (string, error) {
	resp, err := s.client.GetSocVendor(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetBoardModel() (string, error) {
	resp, err := s.client.GetBoardModel(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetBoardVendor() (string, error) {
	resp, err := s.client.GetBoardVendor(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetHardwareVersion() (string, error) {
	resp, err := s.client.GetHardwareVersion(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetHardwareModel() (string, error) {
	resp, err := s.client.GetHardwareModel(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetSystemUuid() (string, error) {
	resp, err := s.client.GetSystemUuid(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetBoardSerial() (string, error) {
	resp, err := s.client.GetBoardSerial(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetCpuSerial() (string, error) {
	resp, err := s.client.GetCpuSerial(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetMachineId() (string, error) {
	resp, err := s.client.GetMachineId(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetArchitecture() (string, error) {
	resp, err := s.client.GetArchitecture(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetTotalRAM() (uint64, error) {
	resp, err := s.client.GetTotalRAM(s.ctx, &types.Empty{})
	if err != nil {
		return 0, err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return 0, err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetCpuModel() (string, error) {
	resp, err := s.client.GetCpuModel(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetCpuCores() (int64, error) {
	resp, err := s.client.GetCpuCores(s.ctx, &types.Empty{})
	if err != nil {
		return 0, err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return 0, err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetCpuThreads() (int64, error) {
	resp, err := s.client.GetCpuThreads(s.ctx, &types.Empty{})
	if err != nil {
		return 0, err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return 0, err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetCpuMinMhz() (float64, error) {
	resp, err := s.client.GetCpuMinMhz(s.ctx, &types.Empty{})
	if err != nil {
		return 0, err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return 0, err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetCpuMaxMhz() (float64, error) {
	resp, err := s.client.GetCpuMaxMhz(s.ctx, &types.Empty{})
	if err != nil {
		return 0, err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return 0, err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetOsName() (string, error) {
	resp, err := s.client.GetOsName(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetOsVersion() (string, error) {
	resp, err := s.client.GetOsVersion(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetKernelVersion() (string, error) {
	resp, err := s.client.GetKernelVersion(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetDistro() (string, error) {
	resp, err := s.client.GetDistro(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetDistroVersion() (string, error) {
	resp, err := s.client.GetDistroVersion(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (s *SystemManager) GetRuntimeVersion() (string, error) {
	resp, err := s.client.GetRuntimeVersion(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

// GetOsRevision returns the logical OS revision string from Gravity (SystemService.GetOsRevision).
func (s *SystemManager) GetOsRevision() (string, error) {
	resp, err := s.client.GetOsRevision(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

// GetBuildVersion is an alias for GetOsRevision (legacy name; proto no longer has GetBuildVersion).
func (s *SystemManager) GetBuildVersion() (string, error) {
	return s.GetOsRevision()
}

// GetRuntimeBuildDate returns the runtime image build date from Gravity (SystemService.GetRuntimeBuildDate).
func (s *SystemManager) GetRuntimeBuildDate() (string, error) {
	resp, err := s.client.GetRuntimeBuildDate(s.ctx, &types.Empty{})
	if err != nil {
		return "", err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

// GetBuildDate is an alias for GetRuntimeBuildDate (legacy name; proto no longer has GetBuildDate).
func (s *SystemManager) GetBuildDate() (string, error) {
	return s.GetRuntimeBuildDate()
}



func (s *SystemManager) GetMetrics() (*systemv26.MetricsInfoResponse, error) {
	resp, err := s.client.GetMetrics(s.ctx, &types.Empty{})
	if err != nil {
		return nil, err
	}
	if err := rpcError(resp.GetError()); err != nil {
		return nil, err
	}
	return resp, nil
}

