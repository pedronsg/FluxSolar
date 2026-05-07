package client

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	aiv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/ai_service/v26"
)

const uploadChunkSize = 256 * 1024 // 256 KB per chunk

// AIManager is the SDK client for AiService (load, infer, unload models).
type AIManager struct {
	client aiv26.AiServiceClient
	ctx    context.Context
}

// NewAIManager constructs an AIManager.
func NewAIManager(client aiv26.AiServiceClient, ctx context.Context) *AIManager {
	return &AIManager{client: client, ctx: ctx}
}

// LoadModel loads a model file (path on the device) into the inference backend.
// Returns the full LoadModelResponse so the caller can read input/output TensorInfo.
func (m *AIManager) LoadModel(modelID, modelPath string, backend aiv26.ModelBackend, execution aiv26.ExecutionMode) (*aiv26.LoadModelResponse, error) {
	ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
	defer cancel()

	resp, err := m.client.LoadModel(ctx, &aiv26.LoadModelRequest{
		ModelId:   modelID,
		ModelPath: modelPath,
		Backend:   backend,
		Execution: execution,
	})
	if err != nil {
		return nil, fmt.Errorf("LoadModel: %w", err)
	}
	if !resp.GetSuccess() {
		if e := resp.GetError(); e != nil {
			return nil, fmt.Errorf("LoadModel: %s", e.GetMessage())
		}
		return nil, fmt.Errorf("LoadModel: server reported failure")
	}
	return resp, nil
}

// UploadAndLoadModel reads a local model file and streams it to the device in
// 256 KB chunks, then loads it into the inference backend.
// Use this when the model file does not yet exist on the device filesystem.
// The model file is stored temporarily on the device and deleted on UnloadModel.
func (m *AIManager) UploadAndLoadModel(modelID, localPath string, backend aiv26.ModelBackend, execution aiv26.ExecutionMode) (*aiv26.LoadModelResponse, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("UploadAndLoadModel: open %q: %w", localPath, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("UploadAndLoadModel: stat %q: %w", localPath, err)
	}

	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute) // large models may be slow
	defer cancel()

	stream, err := m.client.UploadAndLoadModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("UploadAndLoadModel: open stream: %w", err)
	}

	// Send metadata as the first chunk.
	if err := stream.Send(&aiv26.UploadModelChunk{
		Payload: &aiv26.UploadModelChunk_Meta{
			Meta: &aiv26.UploadModelMeta{
				ModelId:    modelID,
				Backend:    backend,
				Execution:  execution,
				TotalBytes: fi.Size(),
				Filename:   filepath.Base(localPath),
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("UploadAndLoadModel: send meta: %w", err)
	}

	// Stream file in chunks.
	buf := make([]byte, uploadChunkSize)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&aiv26.UploadModelChunk{
				Payload: &aiv26.UploadModelChunk_Data{Data: buf[:n]},
			}); sendErr != nil {
				return nil, fmt.Errorf("UploadAndLoadModel: send chunk: %w", sendErr)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("UploadAndLoadModel: read file: %w", readErr)
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, fmt.Errorf("UploadAndLoadModel: %w", err)
	}
	if !resp.GetSuccess() {
		if e := resp.GetError(); e != nil {
			return nil, fmt.Errorf("UploadAndLoadModel: %s", e.GetMessage())
		}
		return nil, fmt.Errorf("UploadAndLoadModel: server reported failure")
	}
	return resp, nil
}

// UnloadModel frees a loaded model from the backend.
func (m *AIManager) UnloadModel(modelID string) error {
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	defer cancel()

	resp, err := m.client.UnloadModel(ctx, &aiv26.UnloadModelRequest{ModelId: modelID})
	if err != nil {
		return fmt.Errorf("UnloadModel: %w", err)
	}
	if !resp.GetSuccess() {
		if e := resp.GetError(); e != nil {
			return fmt.Errorf("UnloadModel: %s", e.GetMessage())
		}
		return fmt.Errorf("UnloadModel: server reported failure")
	}
	return nil
}

// ListModels returns metadata for all currently loaded models.
func (m *AIManager) ListModels() ([]*aiv26.ModelInfo, error) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	resp, err := m.client.ListModels(ctx, &aiv26.ListModelsRequest{})
	if err != nil {
		return nil, fmt.Errorf("ListModels: %w", err)
	}
	return resp.GetModels(), nil
}

// IsModelLoaded returns true if the model is currently loaded on the device,
// along with its tensor schema (inputs/outputs). This is a cheap check that
// avoids a full UploadAndLoadModel round-trip when you just want to confirm the
// model is already resident.
func (m *AIManager) IsModelLoaded(modelID string) (*aiv26.IsModelLoadedResponse, error) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	resp, err := m.client.IsModelLoaded(ctx, &aiv26.IsModelLoadedRequest{ModelId: modelID})
	if err != nil {
		return nil, fmt.Errorf("IsModelLoaded: %w", err)
	}
	return resp, nil
}

// Infer runs one forward pass. The caller controls the timeout via ctx.
// inputData is raw little-endian bytes (float32 or uint8 depending on dtype).
// inputShape is optional; when nil the shape from LoadModel is used server-side.
func (m *AIManager) Infer(ctx context.Context, modelID string, inputData []byte, inputShape []int32, dtype aiv26.TensorDataType) (*aiv26.InferResponse, error) {
	resp, err := m.client.Infer(ctx, &aiv26.InferRequest{
		ModelId:    modelID,
		InputData:  inputData,
		InputShape: inputShape,
		InputDtype: dtype,
	})
	if err != nil {
		return nil, fmt.Errorf("Infer: %w", err)
	}
	if !resp.GetSuccess() {
		if e := resp.GetError(); e != nil {
			return nil, fmt.Errorf("Infer: %s", e.GetMessage())
		}
		return nil, fmt.Errorf("Infer: server reported failure")
	}
	return resp, nil
}
