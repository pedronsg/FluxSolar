package client

import (
	types "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	powerv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/power_service/v26"
	"context"
	"fmt"
)

type PowerManager struct {
    client powerv26.PowerServiceClient
    ctx    context.Context
}

func NewPowerManager(client powerv26.PowerServiceClient, ctx context.Context) *PowerManager {
    return &PowerManager{client: client, ctx: ctx}
}


func (p *PowerManager) Reboot(force bool, reason string) (*PowerResult, error) {
    resp, err := p.client.Reboot(p.ctx, &powerv26.RebootRequest{
        Force:  force,
        Reason: reason,
    })
    if err != nil {
        // Transport or gRPC error
        return nil, err
    }

    result := &PowerResult{
        Success: resp.Error == nil || resp.Error.Code == 0,
        Message: "",
    }

    if resp.Error != nil && resp.Error.Code != 0 {
        // Map error code to a readable string
        codeStr := fmt.Sprintf("%d", resp.Error.Code)
        if name, ok := types.ErrorCode_name[int32(resp.Error.Code)]; ok {
            codeStr = fmt.Sprintf("%s (%d)", name, resp.Error.Code)
        }

        result.Message = fmt.Sprintf("%s (%s)", resp.Error.Message, codeStr)
        return result, fmt.Errorf("reboot failed: %s", result.Message)
    }

    // No error: default success message
    result.Message = "Reboot requested successfully"
    return result, nil
}

func (p *PowerManager) Shutdown(force bool, reason string) (*PowerResult, error) {
    resp, err := p.client.Shutdown(p.ctx, &powerv26.ShutdownRequest{Force: force, Reason: reason})
    if err != nil { return nil, err }
    return &PowerResult{
        Success: resp.Error == nil || resp.Error.Code == 0,
        Message: resp.GetError().GetMessage(),
    }, nil
}
