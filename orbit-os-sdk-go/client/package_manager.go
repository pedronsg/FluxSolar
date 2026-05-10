package client

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	types "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
	pmv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/package_service/v26"
)

const installOrbChunkSize = 256 * 1024

// PackageManager is the SDK client for PackageManagerService (installed apps, install/remove).
type PackageManager struct {
	client pmv26.PackageManagerServiceClient
	ctx    context.Context
}

// NewPackageManager constructs a PackageManager.
func NewPackageManager(client pmv26.PackageManagerServiceClient, ctx context.Context) *PackageManager {
	return &PackageManager{client: client, ctx: ctx}
}

// GetInstalledPackages returns all packages installed on the device (from app_manager).
func (p *PackageManager) GetInstalledPackages() ([]*pmv26.InstalledPackage, error) {
	resp, err := p.client.GetInstalledPackages(p.ctx, &types.Empty{})
	if err != nil {
		return nil, fmt.Errorf("GetInstalledPackages: %w", err)
	}
	if err := rpcError(resp.GetError()); err != nil {
		return nil, err
	}
	return resp.GetPackages(), nil
}

// InstallPackageFromFile streams a local .orb file to Gravity via InstallUpdatePackage (chunked + MD5).
func (p *PackageManager) InstallPackageFromFile(ctx context.Context, orbPath string) error {
	f, err := os.Open(orbPath)
	if err != nil {
		return fmt.Errorf("open package: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat package: %w", err)
	}
	size := info.Size()

	md5Hash := md5.New()
	if _, err := io.Copy(md5Hash, f); err != nil {
		return fmt.Errorf("hash package: %w", err)
	}
	md5Sum := hex.EncodeToString(md5Hash.Sum(nil))
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind package: %w", err)
	}

	var totalChunks int32
	if size == 0 {
		totalChunks = 1
	} else {
		totalChunks = int32((size + int64(installOrbChunkSize) - 1) / int64(installOrbChunkSize))
	}

	stream, err := p.client.InstallUpdatePackage(ctx)
	if err != nil {
		return fmt.Errorf("InstallUpdatePackage: %w", err)
	}

	base := filepath.Base(orbPath)
	for i := int32(1); i <= totalChunks; i++ {
		var readLen int64
		if size == 0 {
			readLen = 0
		} else {
			readLen = int64(installOrbChunkSize)
			remaining := size - int64(i-1)*int64(installOrbChunkSize)
			if readLen > remaining {
				readLen = remaining
			}
		}
		buf := make([]byte, readLen)
		if readLen > 0 {
			if _, err := io.ReadFull(f, buf); err != nil {
				return fmt.Errorf("read chunk %d: %w", i, err)
			}
		}
		ch := &pmv26.PackageChunk{
			Filename:    base,
			ChunkNumber: i,
			TotalChunks: totalChunks,
			Data:        buf,
			IsLast:      i == totalChunks,
			FileMd5:     md5Sum,
			FileSize:    size,
		}
		if err := stream.Send(ch); err != nil {
			return fmt.Errorf("send chunk %d: %w", i, err)
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("InstallUpdatePackage close: %w", err)
	}
	if err := rpcError(resp.GetError()); err != nil {
		return err
	}
	if !resp.GetValue() {
		return fmt.Errorf("InstallUpdatePackage: server returned false")
	}
	return nil
}

// RemovePackage uninstalls a package on the device (Gravity PackageManagerService.RemovePackage).
// packageID is the package_id as reported by GetInstalledPackages (InstalledPackage.PackageId).
func (p *PackageManager) RemovePackage(ctx context.Context, packageID string) error {
	packageID = strings.TrimSpace(packageID)
	if packageID == "" {
		return fmt.Errorf("RemovePackage: empty package_id")
	}
	resp, err := p.client.RemovePackage(ctx, &pmv26.RemovePackageRequest{PackageId: packageID})
	if err != nil {
		return fmt.Errorf("RemovePackage: %w", err)
	}
	if err := rpcError(resp.GetError()); err != nil {
		return err
	}
	if !resp.GetValue() {
		return fmt.Errorf("RemovePackage: server returned false")
	}
	return nil
}
