// Package metadata merges per-binary AppManifest (from metadata.json next to main) with linker-injected fields.
//
// EntryPoint, PackageType, BuildArch (target GOARCH), BuildDate, GitCommit are injected by the build — use -ldflags:
//
//	-X github.com/OrbitOS-org/orbit-os-sdk-go/v26/metadata.EntryPoint=basic
//	-X github.com/OrbitOS-org/orbit-os-sdk-go/v26/metadata.PackageType=binary
//	-X github.com/OrbitOS-org/orbit-os-sdk-go/v26/metadata.BuildArch=arm64
//
// build_package.sh / CI set these; users must not invent type/entry point in metadata.json.
//
//	go build -ldflags "-X github.com/OrbitOS-org/orbit-os-sdk-go/v26/metadata.EntryPoint=basic -X github.com/OrbitOS-org/orbit-os-sdk-go/v26/metadata.BuildArch=arm64 ..." ./cmd/examples/basic
package metadata

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/logger"
)

const logTag = "metadata"

// AppMetadata is the full manifest for logging / deployment (embedded file + linker stamps).
// JSON key order matches ORB manifest.json from build_package.sh.
type AppMetadata struct {
	PackageId    string   `json:"package_id"`
	Version      string   `json:"version"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Type         string   `json:"type"`
	Architecture string   `json:"architecture,omitempty"` // target GOARCH; -X BuildArch=…
	EntryPoint   string   `json:"entry_point"`              // binary basename; -X EntryPoint=…
	BuildDate    string   `json:"build_date,omitempty"`
	GitCommit    string   `json:"git_commit,omitempty"`
	Permissions  []string `json:"permissions,omitempty"`
}

// AppManifest is the static part read from metadata.json (//go:embed) or tests; ORB packaging uses the same file.
// Entry point and package type at build time come from the linker, not from this struct.
type AppManifest struct {
	PackageId   string
	Name        string
	Version     string
	Description string
	Permissions []string
}

type metadataFile struct {
	PackageId   string   `json:"package_id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions,omitempty"`
	Data        string   `json:"data,omitempty"` // packaging: relative path under package (e.g. ./workdir) → ORB data/ root
	// entry_point, orb_slug are for packaging only.
}

// ParseAppManifestJSON decodes metadata.json (same fields as AppManifest).
func ParseAppManifestJSON(data []byte) (AppManifest, error) {
	var f metadataFile
	if err := json.Unmarshal(data, &f); err != nil {
		return AppManifest{}, err
	}
	if f.PackageId == "" || f.Name == "" || f.Description == "" {
		return AppManifest{}, fmt.Errorf("metadata.json: package_id, name, and description are required")
	}
	return AppManifest{
		PackageId:   f.PackageId,
		Name:        f.Name,
		Version:     f.Version,
		Description: f.Description,
		Permissions: f.Permissions,
	}, nil
}

// MustParseAppManifestJSON is for package-level var init with //go:embed metadata.json
func MustParseAppManifestJSON(data []byte) AppManifest {
	m, err := ParseAppManifestJSON(data)
	if err != nil {
		panic(fmt.Sprintf("metadata: metadata.json: %v", err))
	}
	return m
}

// Linker-injected (build / packaging).
var (
	BuildDate   = "unknown"
	GitCommit   = "unknown"
	EntryPoint  = ""       // -X github.com/OrbitOS-org/orbit-os-sdk-go/v26/metadata.EntryPoint=<binary basename>
	PackageType = "binary" // -X github.com/OrbitOS-org/orbit-os-sdk-go/v26/metadata.PackageType=… (default binary)
	BuildArch   = ""       // -X github.com/OrbitOS-org/orbit-os-sdk-go/v26/metadata.BuildArch=<goarch> (e.g. arm64)
)

// Build returns AppMetadata. It does not log.
func Build(m AppManifest) AppMetadata {

	t := PackageType
	if t == "" {
		t = "binary"
	}
	arch := BuildArch
	if arch == "" {
		arch = runtime.GOARCH
	}
	return AppMetadata{
		PackageId:    m.PackageId,
		Version:      m.Version,
		Name:         m.Name,
		Description:  m.Description,
		Type:         t,
		Architecture: arch,
		EntryPoint:   EntryPoint,
		BuildDate:    BuildDate,
		GitCommit:    GitCommit,
		Permissions:  m.Permissions,
	}
}

func (m AppManifest) PrintInfo() {
	perms := "(none)"
	if len(m.Permissions) > 0 {
		perms = strings.Join(m.Permissions, ", ")
	}
	logger.Infof(logTag, "--- App metadata -------------------------------------------")
	logger.Infof(logTag, " Package ID      : %s", m.PackageId)
	logger.Infof(logTag, " Name            : %s", m.Name)
	logger.Infof(logTag, " Version         : %s", m.Version)
	logger.Infof(logTag, " Description     : %s", m.Description)
	logger.Infof(logTag, " Permissions     : %s", perms)
	logger.Infof(logTag, "------------------------------------------------------------")
}
