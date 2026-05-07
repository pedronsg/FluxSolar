#!/bin/bash
set -e

echo "🔧 Generating gRPC code..."

# Go to this script's directory
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Repo root (orbit-os-sdk-go). This script lives at api/proto/gen.sh
SDK_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# Generated .pb.go files: api/<service>/v26/…
OUTPUT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Proto tree: GRAVITY_API_PROTO overrides (absolute or relative path to repo root).
# Else try ../../ then ../../../ (standalone …/SDKs/orbit-os-sdk-go vs submódulo …/workspace/orbit-os-sdk-go).
PROTO_REPO_DIR=""
if [ -n "${GRAVITY_API_PROTO:-}" ] && [ -d "$GRAVITY_API_PROTO" ]; then
	PROTO_REPO_DIR="$(cd "$GRAVITY_API_PROTO" && pwd)"
else
	for candidate in "$SDK_ROOT/../../gravity-api-proto" "$SDK_ROOT/../../../gravity-api-proto"; do
		if [ -d "$candidate" ]; then
			PROTO_REPO_DIR="$(cd "$candidate" && pwd)"
			break
		fi
	done
fi

if [ -z "$PROTO_REPO_DIR" ] || [ ! -d "$PROTO_REPO_DIR" ]; then
	echo "❌ Error: gravity-api-proto not found."
	echo "   Tried: \$SDK_ROOT/../../gravity-api-proto then \$SDK_ROOT/../../../gravity-api-proto"
	echo "   (SDK_ROOT=$SDK_ROOT)"
	echo "   Clone gravity-api-proto or set GRAVITY_API_PROTO to the protos repo root."
	exit 1
fi

echo "📂 Module root: $SDK_ROOT"
echo "📂 Protos:       $PROTO_REPO_DIR"
echo "📂 Output:       $OUTPUT_DIR"

# Must match: go.mod module path + /api (see generated .pb.go imports)
GO_IMPORT_PREFIX="github.com/OrbitOS-org/orbit-os-sdk-go/v26/api"

# Target protoc version (post-3.21 versioning: v25.x ≈ old 3.25)
PROTOC_TARGET_VERSION="25.3"       # Version to install
PROTOC_MIN_SEMVER="25.0"           # Minimum acceptable
PROTOC_BIN=""
PROTOC_VERSION=""

# Check if protoc is installed and read version
if command -v protoc &> /dev/null; then
    PROTOC_BIN="protoc"
    PROTOC_VERSION=$(protoc --version 2>/dev/null | awk '{print $2}' | tr -d '[:space:]' || echo "0")
fi

# Install protoc v25.x from GitHub
# Note: after v3.21.x, protobuf uses v22, v23, v24, v25...
# Old "protoc 3.25" maps to "protoc v25.x" in the new scheme.
install_protoc() {
    echo "📥 Installing protoc v$PROTOC_TARGET_VERSION..."

    # Require curl or wget
    DOWNLOAD_CMD=""
    if command -v curl &> /dev/null; then
        DOWNLOAD_CMD="curl"
    elif command -v wget &> /dev/null; then
        DOWNLOAD_CMD="wget"
    else
        echo "❌ Error: curl or wget not found."
        echo "   Install manually: sudo apt-get install -y curl unzip"
        exit 1
    fi

    if ! command -v unzip &> /dev/null; then
        echo "❌ Error: unzip not found."
        echo "   Install with: sudo apt-get install -y unzip"
        exit 1
    fi

    # Detect CPU architecture
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)   PROTOC_ARCH="x86_64" ;;
        aarch64|arm64) PROTOC_ARCH="aarch_64" ;;
        *)
            echo "❌ Error: unsupported architecture $ARCH."
            exit 1
            ;;
    esac

    BIN_DIR="$HOME/.local/bin"
    mkdir -p "$BIN_DIR"
    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    # Try v25.x releases
    VERSIONS_TO_TRY=("25.3" "25.2" "25.1" "25.0")
    PROTOC_URL=""
    ACTUAL_VERSION=""

    echo "   Looking for a release on GitHub..."
    for VER in "${VERSIONS_TO_TRY[@]}"; do
        TEST_URL="https://github.com/protocolbuffers/protobuf/releases/download/v${VER}/protoc-${VER}-linux-${PROTOC_ARCH}.zip"
        if [ "$DOWNLOAD_CMD" = "curl" ]; then
            HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -L "$TEST_URL" 2>/dev/null || echo "000")
        else
            HTTP_CODE=$(wget --server-response --spider "$TEST_URL" 2>&1 | grep "HTTP/" | tail -1 | awk '{print $2}' || echo "000")
        fi

        if [ "$HTTP_CODE" = "200" ]; then
            PROTOC_URL="$TEST_URL"
            ACTUAL_VERSION="$VER"
            echo "   ✓ Found version v$VER"
            break
        fi
    done

    if [ -z "$PROTOC_URL" ]; then
        echo ""
        echo "❌ Could not download protoc automatically."
        echo ""
        echo "     sudo apt-get update && sudo apt-get install -y protobuf-compiler"
        echo ""
        echo "   Or download manually from:"
        echo "     https://github.com/protocolbuffers/protobuf/releases"
        echo "   and copy the binary to ~/.local/bin/protoc"
        exit 1
    fi

    echo "   Downloading protoc v$ACTUAL_VERSION..."
    if [ "$DOWNLOAD_CMD" = "curl" ]; then
        curl -L -f -o "$TEMP_DIR/protoc.zip" "$PROTOC_URL" 2>&1
    else
        wget -O "$TEMP_DIR/protoc.zip" "$PROTOC_URL" 2>&1
    fi

    cd "$TEMP_DIR"
    unzip -q protoc.zip || { echo "❌ Failed to extract protoc.zip"; exit 1; }

    [ -f "bin/protoc" ] || { echo "❌ bin/protoc not found in zip"; exit 1; }

    cp bin/protoc "$BIN_DIR/protoc"
    chmod +x "$BIN_DIR/protoc"
    export PATH="$BIN_DIR:$PATH"
    PROTOC_BIN="$BIN_DIR/protoc"

    INSTALLED_VERSION=$($PROTOC_BIN --version 2>/dev/null | awk '{print $2}' || echo "")
    [ -n "$INSTALLED_VERSION" ] || { echo "❌ Installed protoc binary does not work"; exit 1; }

    echo "✓ protoc $INSTALLED_VERSION installed in $BIN_DIR"
    echo "   Tip: add to ~/.bashrc: export PATH=\"\$HOME/.local/bin:\$PATH\""
}

# Ensure protoc v25.x is available under ~/.local/bin
LOCAL_PROTOC="$HOME/.local/bin/protoc"
NEEDS_INSTALL=true

# Check if protoc v25.x is already installed under ~/.local/bin
if [ -f "$LOCAL_PROTOC" ]; then
    LOCAL_VERSION=$($LOCAL_PROTOC --version 2>/dev/null | awk '{print $2}' | tr -d '[:space:]' || echo "")
    # New scheme uses 25.x directly (not 3.25.x)
    SMALLER=$(printf '%s\n' "$LOCAL_VERSION" "$PROTOC_MIN_SEMVER" | sort -V | head -n1 | tr -d '[:space:]')
    if [ -n "$LOCAL_VERSION" ] && { [ "$SMALLER" != "$LOCAL_VERSION" ] || [ "$LOCAL_VERSION" = "$PROTOC_MIN_SEMVER" ]; }; then
        echo "✓ protoc v$LOCAL_VERSION already installed in ~/.local/bin"
        export PATH="$HOME/.local/bin:$PATH"
        PROTOC_BIN="$LOCAL_PROTOC"
        NEEDS_INSTALL=false
    fi
fi

# Install if version is too old or missing
if [ "$NEEDS_INSTALL" = true ]; then
    if [ -n "$PROTOC_VERSION" ] && [ "$PROTOC_VERSION" != "0" ]; then
        echo "⚠️  System has protoc $PROTOC_VERSION. Installing protoc v$PROTOC_TARGET_VERSION (new scheme)..."
    else
        echo "⚠️  protoc not found. Installing protoc v$PROTOC_TARGET_VERSION..."
    fi
    install_protoc
fi

# Prefer the correct protoc build
# Prepend ~/.local/bin to PATH if missing
if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
    export PATH="$HOME/.local/bin:$PATH"
fi

# If we installed a specific protoc, prefer its directory on PATH
if [ -n "$PROTOC_BIN" ] && [ "$PROTOC_BIN" != "protoc" ]; then
    export PATH="$(dirname "$PROTOC_BIN"):$PATH"
fi

# Print final protoc version
FINAL_PROTOC_VERSION=$(protoc --version 2>/dev/null | awk '{print $2}' || echo "")
if [ -n "$FINAL_PROTOC_VERSION" ]; then
    echo "✓ Using protoc version $FINAL_PROTOC_VERSION"
fi

# Ensure Go protoc plugins
if [ ! -f "$HOME/go/bin/protoc-gen-go" ]; then
    echo "⚠️  protoc-gen-go not found. Installing..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
fi

if [ ! -f "$HOME/go/bin/protoc-gen-go-grpc" ]; then
    echo "⚠️  protoc-gen-go-grpc not found. Installing..."
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

PATH="$HOME/go/bin:$PATH"

mkdir -p "$OUTPUT_DIR"

# --- Generate common (types.proto, api_info.proto if present) ---
if [ -d "$PROTO_REPO_DIR/common" ] && [ "$(ls -A $PROTO_REPO_DIR/common/*.proto 2>/dev/null)" ]; then
    echo "📦 Generating common..."
    mkdir -p "$OUTPUT_DIR/common"
    
    cd "$SCRIPT_DIR"
    protoc -I "$PROTO_REPO_DIR" \
        --go_out="$OUTPUT_DIR/common" --go_opt=paths=source_relative \
        --go_opt=Mcommon/types.proto=${GO_IMPORT_PREFIX}/common \
        --go_opt=Mcommon/api_info.proto=${GO_IMPORT_PREFIX}/common \
        --go-grpc_out="$OUTPUT_DIR/common" --go-grpc_opt=paths=source_relative \
        --go-grpc_opt=Mcommon/types.proto=${GO_IMPORT_PREFIX}/common \
        --go-grpc_opt=Mcommon/api_info.proto=${GO_IMPORT_PREFIX}/common \
        "$PROTO_REPO_DIR/common"/*.proto
    
    # Move files if protoc emitted an extra common/ directory
    if [ -d "$OUTPUT_DIR/common/common" ]; then
        mv "$OUTPUT_DIR/common/common"/* "$OUTPUT_DIR/common/" 2>/dev/null || true
        rmdir "$OUTPUT_DIR/common/common" 2>/dev/null || true
    fi
    
    echo "  ✓ Common generated"
fi

# --- Generate one service/version ---
generate_service_version() {
    local SERVICE=$1
    local VERSION=$2
    local SRC_DIR="$PROTO_REPO_DIR/$SERVICE/$VERSION"
    local OUT_DIR="$OUTPUT_DIR/$SERVICE/$VERSION"
    
    if [ ! -d "$SRC_DIR" ]; then
        return
    fi
    
    if [ -z "$(ls -A "$SRC_DIR"/*.proto 2>/dev/null)" ]; then
        return
    fi
    
    echo "📦 Generating $SERVICE/$VERSION..."
    mkdir -p "$OUT_DIR"
    
    cd "$SCRIPT_DIR"
    # Go import path under api/
    GO_PKG="${GO_IMPORT_PREFIX}/$SERVICE/$VERSION"
    
    # -I proto repo root so common/types.proto imports resolve
    for proto_file in "$SRC_DIR"/*.proto; do
        if [ -f "$proto_file" ]; then
            # Proto file basename
            PROTO_NAME=$(basename "$proto_file")
            REL_PATH="$SERVICE/$VERSION/$PROTO_NAME"
            
            protoc -I "$PROTO_REPO_DIR" \
                --go_out="$OUT_DIR" --go_opt=paths=source_relative \
                --go_opt=M"$REL_PATH=$GO_PKG" \
                --go_opt=Mcommon/types.proto=${GO_IMPORT_PREFIX}/common \
                --go_opt=Mcommon/api_info.proto=${GO_IMPORT_PREFIX}/common \
                --go-grpc_out="$OUT_DIR" --go-grpc_opt=paths=source_relative \
                --go-grpc_opt=M"$REL_PATH=$GO_PKG" \
                --go-grpc_opt=Mcommon/types.proto=${GO_IMPORT_PREFIX}/common \
                --go-grpc_opt=Mcommon/api_info.proto=${GO_IMPORT_PREFIX}/common \
                "$proto_file"
        fi
    done
    
    # Flatten extra service/version dirs if protoc nested them
    if [ -d "$OUT_DIR/$SERVICE/$VERSION" ]; then
        mv "$OUT_DIR/$SERVICE/$VERSION"/* "$OUT_DIR/" 2>/dev/null || true
        rm -rf "$OUT_DIR/$SERVICE" 2>/dev/null || true
    fi
    
    echo "  ✓ $SERVICE/$VERSION generated"
}

# --- Walk gravity-api-proto for *_service/v* ---
for SERVICE_DIR in "$PROTO_REPO_DIR"/*_service; do
    if [ -d "$SERVICE_DIR" ]; then
        SERVICE=$(basename "$SERVICE_DIR")
        for VERSION_DIR in "$SERVICE_DIR"/v*; do
            if [ -d "$VERSION_DIR" ]; then
                VERSION=$(basename "$VERSION_DIR")
                generate_service_version "$SERVICE" "$VERSION"
            fi
        done
    fi
done

echo ""
echo "✅ gRPC code generated successfully!"
echo ""
echo "Generated files:"
find "$OUTPUT_DIR" -name "*.pb.go" -type f 2>/dev/null | while read file; do
    size=$(ls -lh "$file" | awk '{print $5}')
    echo "  - $file ($size)"
done
