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
	updatesvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/update_service/v26"
)

type UpdateManager struct {
	client updatesvcv26.UpdateServiceClient
	ctx    context.Context
}

func NewUpdateManager(client updatesvcv26.UpdateServiceClient, ctx context.Context) *UpdateManager {
	return &UpdateManager{client: client, ctx: ctx}
}

// FactoryReset resets the device to factory defaults and reboots.
func (u *UpdateManager) FactoryReset() (bool, error) {
	resp, err := u.client.FactoryReset(u.ctx, &types.Empty{})
	if err != nil {
		return false, err
	}
	return resp.GetValue(), rpcError(resp.GetError())
}

const installOtaChunkSize = 256 * 1024

// InstallOtaFromFile streams a local OTA image file to Gravity via UpdateService.Update (chunked + MD5).
func (u *UpdateManager) InstallOtaFromFile(ctx context.Context, otaPath string) error {
	f, err := os.Open(otaPath)
	if err != nil {
		return fmt.Errorf("open OTA file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat OTA file: %w", err)
	}
	size := info.Size()

	md5Hash := md5.New()
	if _, err := io.Copy(md5Hash, f); err != nil {
		return fmt.Errorf("hash OTA file: %w", err)
	}
	md5Sum := hex.EncodeToString(md5Hash.Sum(nil))
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind OTA file: %w", err)
	}

	var totalChunks int32
	if size == 0 {
		totalChunks = 1
	} else {
		totalChunks = int32((size + int64(installOtaChunkSize) - 1) / int64(installOtaChunkSize))
	}

	stream, err := u.client.Update(ctx)
	if err != nil {
		return fmt.Errorf("UpdateService.Update: %w", err)
	}

	base := filepath.Base(otaPath)
	if !strings.HasSuffix(strings.ToLower(base), ".orbit") {
		base = strings.TrimSuffix(base, filepath.Ext(base)) + ".orbit"
	}

	for i := int32(1); i <= totalChunks; i++ {
		readLen := int64(installOtaChunkSize)
		remaining := size - int64(i-1)*int64(installOtaChunkSize)
		if size == 0 {
			readLen = 0
		} else if readLen > remaining {
			readLen = remaining
		}
		buf := make([]byte, readLen)
		if readLen > 0 {
			if _, err := io.ReadFull(f, buf); err != nil {
				return fmt.Errorf("read OTA chunk %d: %w", i, err)
			}
		}
		ch := &updatesvcv26.FileChunk{
			Filename:    base,
			ChunkNumber: i,
			TotalChunks: totalChunks,
			Data:        buf,
			IsLast:      i == totalChunks,
			FileMd5:     md5Sum,
			FileSize:    size,
		}
		if err := stream.Send(ch); err != nil {
			return fmt.Errorf("send OTA chunk %d: %w", i, err)
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("UpdateService.Update close: %w", err)
	}
	if err := rpcError(resp.GetError()); err != nil {
		return err
	}
	if !resp.GetValue() {
		return fmt.Errorf("UpdateService.Update: device returned false")
	}
	return nil
}
