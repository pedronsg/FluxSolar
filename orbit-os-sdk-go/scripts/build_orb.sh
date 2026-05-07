#!/usr/bin/env bash
#
# Cross-build any Go main package and produce an ORB (bin + manifest.json + …).
# Shipped with orbit-os-sdk-go; builds apps that live in orbit-os-workspace-go (or any repo with cmd/examples).
#
# Package fields for manifest.json come from <package>/metadata.json — the same file is embedded
# in the binary (//go:embed) and parsed by metadata.ParseAppManifestJSON. Build stamps: -ldflags.
#
# Usage (from orbit-os-workspace-go repo root, with orbit-os-sdk-go as submodule):
#   ./orbit-os-sdk-go/scripts/build_package.sh
#   ./orbit-os-sdk-go/scripts/build_package.sh -path basic -arch amd64
#
# If the SDK is not nested under the app repo, set ORBIT_PROJECT_ROOT to the app repository root
# (directory that contains cmd/examples and go.mod).
#
# Options:
#   -path, --path <dir>   main package (from app repo root, or short name → cmd/examples/<name>)
#   -arch, --arch <goarch> GOARCH (default: arm64, or $GOARCH)
#   -os, --os <goos>       GOOS (default: linux, or $GOOS)
#
# Version comes only from metadata.json "version" — not from CLI.
# ORB output: <app repo>/packages/<entry_point>_v<version>.orb (staging under packages/build/).
# Bundle extra files: if <package>/orb/ exists and is non-empty, its contents are copied to the ORB root (next to manifest.json, bin/, …).
# Otherwise, optional metadata.json "data" (legacy "workdir"): relative path under the main package (e.g. "./workdir") → packed as data/ in ORB.
#
# Env:
#   ORBIT_PROJECT_ROOT — app repo root (required if cmd/examples is not at ../.. from this script)
#   ORBIT_METADATA_FILE — override path to metadata JSON (default: <package>/metadata.json)
#   ORBIT_IDENTITY_FILE — legacy alias for ORBIT_METADATA_FILE
#   ENTRY_POINT         — override binary basename (default: entry_point in metadata.json or dir name)
#   PACKAGE_TYPE        — default binary
#   GOOS GOARCH CGO_ENABLED — defaults if not set via -os / -arch
#
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

usage() {
	echo "Usage: $0 [options] [package]" >&2
	echo "" >&2
	echo "Options:" >&2
	echo "  -path, --path <dir>    main package path or short name (default: ./cmd/examples/basic)" >&2
	echo "  -arch, --arch <goarch> GOARCH (default: arm64)" >&2
	echo "  -os, --os <goos>       GOOS (default: linux)" >&2
	echo "  -h, --help             this text" >&2
	echo "" >&2
	echo "Version is read only from metadata.json (field \"version\"), not from the command line." >&2
	echo "Positional package is used when -path is not given." >&2
	echo "Set ORBIT_PROJECT_ROOT if your app repo is not the parent of orbit-os-sdk-go/." >&2
	exit 2
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SDK_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Application module root: contains cmd/examples, go.mod (app metadata types live in orbit-os-sdk-go/metadata)
if [[ -n "${ORBIT_PROJECT_ROOT:-}" ]]; then
	PROJECT_ROOT="$(cd "$ORBIT_PROJECT_ROOT" && pwd)"
elif [[ -d "$(cd "$SCRIPT_DIR/../.." && pwd)/cmd/examples" ]]; then
	PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
else
	echo -e "${RED}Cannot find app repository (expected cmd/examples).${NC}" >&2
	echo "  Clone layout: <app>/orbit-os-sdk-go/scripts/this-script.sh" >&2
	echo "  Or set: export ORBIT_PROJECT_ROOT=/path/to/orbit-os-workspace-go" >&2
	exit 1
fi

MODULE="$(grep -m1 '^module ' "$PROJECT_ROOT/go.mod" | awk '{print $2}' | tr -d '\r')"
if [[ -z "$MODULE" ]]; then
	echo -e "${RED}Could not read module path from ${PROJECT_ROOT}/go.mod${NC}" >&2
	exit 1
fi

SDK_MODULE="$(grep -m1 '^module ' "$SDK_ROOT/go.mod" | awk '{print $2}' | tr -d '\r')"
if [[ -z "$SDK_MODULE" ]]; then
	echo -e "${RED}Could not read module path from ${SDK_ROOT}/go.mod${NC}" >&2
	exit 1
fi

PACKAGES_DIR="$PROJECT_ROOT/packages"
BUILD_DIR="$PACKAGES_DIR/build"
OUTPUT_DIR="$PACKAGES_DIR"

GO_PKG_ARG=""
GOARCH_ARG=""
GOOS_ARG=""
POS=()

while [[ $# -gt 0 ]]; do
	case "$1" in
	-path | --path)
		[[ -n "${2:-}" ]] || {
			echo -e "${RED}${1}: missing value${NC}" >&2
			exit 2
		}
		GO_PKG_ARG="$2"
		shift 2
		;;
	-ver | --ver | --version)
		echo -e "${RED}Version must be set in metadata.json only (remove ${1}).${NC}" >&2
		exit 2
		;;
	-arch | --arch)
		[[ -n "${2:-}" ]] || {
			echo -e "${RED}${1}: missing value${NC}" >&2
			exit 2
		}
		GOARCH_ARG="$2"
		shift 2
		;;
	-os | --os)
		[[ -n "${2:-}" ]] || {
			echo -e "${RED}${1}: missing value${NC}" >&2
			exit 2
		}
		GOOS_ARG="$2"
		shift 2
		;;
	-h | --help)
		usage
		;;
	--)
		shift
		while [[ $# -gt 0 ]]; do
			POS+=("$1")
			shift
		done
		break
		;;
	-*)
		echo -e "${RED}Unknown option: $1${NC}" >&2
		usage
		;;
	*)
		POS+=("$1")
		shift
		;;
	esac
done

# Positional fallback: [package]
if [[ -z "$GO_PKG_ARG" && ${#POS[@]} -ge 1 ]]; then
	GO_PKG_ARG="${POS[0]}"
fi
if [[ ${#POS[@]} -gt 1 ]]; then
	echo -e "${RED}Too many positional arguments (expected at most: package). Set version in metadata.json.${NC}" >&2
	exit 2
fi

if [[ -n "${GO_PKG_ARG:-}" ]]; then
	if [[ "$GO_PKG_ARG" == */* || "$GO_PKG_ARG" == ./* ]]; then
		GO_PKG="${GO_PKG_ARG%/}"
	else
		GO_PKG="./cmd/examples/$GO_PKG_ARG"
	fi
else
	GO_PKG="./cmd/examples/basic"
fi

INVOCATION_CWD="$(pwd)"
cd "$PROJECT_ROOT"
# Paths are resolved from app repo root. Also accept paths under cmd/ only, e.g. ./examples/basic → cmd/examples/basic
if [[ ! -d "$GO_PKG" ]]; then
	GP="${GO_PKG#./}"
	if [[ -d "$PROJECT_ROOT/$GP" ]]; then
		GO_PKG="$PROJECT_ROOT/$GP"
	elif [[ -d "$PROJECT_ROOT/cmd/$GP" ]]; then
		GO_PKG="$PROJECT_ROOT/cmd/$GP"
	fi
fi
# Relative paths from the caller's cwd (e.g. ../../cmd/... from orbit-os-sdk-go/scripts/)
if [[ ! -d "$GO_PKG" ]] && [[ "$GO_PKG" != /* ]] && [[ -d "$INVOCATION_CWD/$GO_PKG" ]]; then
	GO_PKG="$(cd "$INVOCATION_CWD/$GO_PKG" && pwd)"
fi
# Last segment may be entry_point (binary name), not a subdir — use parent if it has main.go
if [[ ! -d "$GO_PKG" ]] && [[ "$GO_PKG" != /* ]]; then
	PARENT="$(dirname "$GO_PKG")"
	if [[ -n "$PARENT" && "$PARENT" != . ]] && [[ -d "$INVOCATION_CWD/$PARENT" ]] && [[ -f "$INVOCATION_CWD/$PARENT/main.go" ]]; then
		GO_PKG="$(cd "$INVOCATION_CWD/$PARENT" && pwd)"
	fi
fi
if [[ ! -d "$GO_PKG" ]]; then
	PARENT="$(dirname "$GO_PKG")"
	if [[ -n "$PARENT" && "$PARENT" != . && "$PARENT" != / ]] && [[ -d "$PARENT" ]] && [[ -f "$PARENT/main.go" ]]; then
		GO_PKG="$PARENT"
	fi
fi
if [[ ! -d "$GO_PKG" ]]; then
	echo -e "${RED}Not a directory: ${GO_PKG}${NC}" >&2
	echo "  Use the package directory (where main.go lives), e.g. ./cmd/examples/gpio_output, short name gpio_output, or -path gpio_output." >&2
	echo "  Do not append the binary basename (entry_point from metadata.json) to the path." >&2
	exit 1
fi

GO_PKG_ABS="$(cd "$GO_PKG" && pwd)"
PKG_DIR_NAME="$(basename "$GO_PKG_ABS")"
if [[ -n "${ORBIT_METADATA_FILE:-}" ]]; then
	METADATA_FILE="$ORBIT_METADATA_FILE"
elif [[ -n "${ORBIT_IDENTITY_FILE:-}" ]]; then
	METADATA_FILE="$ORBIT_IDENTITY_FILE"
else
	METADATA_FILE="$GO_PKG_ABS/metadata.json"
fi

if [[ ! -f "$METADATA_FILE" ]]; then
	echo -e "${RED}Missing metadata file: ${METADATA_FILE}${NC}" >&2
	echo "  Add metadata.json next to main (see cmd/examples/basic/metadata.json) or set ORBIT_METADATA_FILE." >&2
	exit 1
fi

# shellcheck disable=SC1090
eval "$(METADATA_LOAD_PATH="$METADATA_FILE" METADATA_DEFAULT_ENTRY="$PKG_DIR_NAME" python3 <<'PY'
import json, os, sys, shlex
path = os.environ["METADATA_LOAD_PATH"]
default_entry = os.environ["METADATA_DEFAULT_ENTRY"]
with open(path, encoding="utf-8") as f:
    d = json.load(f)
for k in ("package_id", "name", "version", "description"):
    if k not in d:
        print(f"missing {k!r} in {path}", file=sys.stderr)
        sys.exit(1)
if not str(d.get("version", "")).strip():
    print(f"metadata.json: non-empty \"version\" is required in {path}", file=sys.stderr)
    sys.exit(1)
entry = (d.get("entry_point") or default_entry).strip()
if not entry:
    print("entry_point (or directory name) must be non-empty", file=sys.stderr)
    sys.exit(1)
for k in ("package_id", "name", "version", "description"):
    print(f"export METADATA_{k.upper()}={shlex.quote(str(d[k]))}")
print(f"export METADATA_ENTRY_POINT={shlex.quote(entry)}")
print(f"export METADATA_PERMISSIONS={shlex.quote(json.dumps(d.get('permissions', [])))}")
_pack = d.get("data")
if _pack is None:
    _pack = d.get("workdir")
if _pack is not None and str(_pack).strip():
    print(f"export METADATA_WORKDIR={shlex.quote(str(_pack).strip())}")
else:
    print("export METADATA_WORKDIR=")
PY
)"

ENTRY_POINT="${ENTRY_POINT:-$METADATA_ENTRY_POINT}"

# ORB + linker version: only from metadata.json (same as embedded app metadata).
RELEASE_VERSION="$METADATA_VERSION"
if [[ -z "$RELEASE_VERSION" ]]; then
	echo -e "${RED}metadata.json: \"version\" must be non-empty${NC}" >&2
	exit 1
fi

echo -e "${GREEN}=== Building ${METADATA_PACKAGE_ID} ${RELEASE_VERSION} — ${GO_PKG} ===${NC}"

echo -e "${YELLOW}Cleaning previous build...${NC}"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR/bin" "$BUILD_DIR/lib" "$BUILD_DIR/META-INF"

BUILD_DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
GIT_COMMIT="$(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")"

PACKAGE_TYPE="${PACKAGE_TYPE:-binary}"

export GOARCH="${GOARCH_ARG:-${GOARCH:-arm64}}"
export GOOS="${GOOS_ARG:-${GOOS:-linux}}"
export CGO_ENABLED="${CGO_ENABLED:-0}"

# Linker stamps (version comes from metadata.json in code; BuildArch = target GOARCH for this build).
LDFLAGS="-X ${SDK_MODULE}/metadata.BuildDate=${BUILD_DATE} -X ${SDK_MODULE}/metadata.GitCommit=${GIT_COMMIT} -X ${SDK_MODULE}/metadata.EntryPoint=${ENTRY_POINT} -X ${SDK_MODULE}/metadata.PackageType=${PACKAGE_TYPE} -X ${SDK_MODULE}/metadata.BuildArch=${GOARCH}"
LDFLAGS_STRIP="${LDFLAGS} -s -w"

echo -e "${YELLOW}Compiling ${ENTRY_POINT} for ${GOOS}/${GOARCH}...${NC}"
	go build \
	-ldflags="$LDFLAGS_STRIP" \
	-trimpath \
	-o "$BUILD_DIR/bin/${ENTRY_POINT}" \
	"$GO_PKG_ABS"

if [[ ! -f "$BUILD_DIR/bin/${ENTRY_POINT}" ]]; then
	echo -e "${RED}Error: failed to compile ${ENTRY_POINT}${NC}"
	exit 1
fi
echo -e "${GREEN}✓ Binary built${NC}"

if command -v upx &>/dev/null; then
	echo -e "${YELLOW}Compressing binary with UPX...${NC}"
	BINARY_PATH="$BUILD_DIR/bin/${ENTRY_POINT}"
	ORIGINAL_SIZE="$(du -h "$BINARY_PATH" | cut -f1)"
	if upx --best --lzma "$BINARY_PATH" &>/dev/null; then
		COMPRESSED_SIZE="$(du -h "$BINARY_PATH" | cut -f1)"
		echo -e "${GREEN}✓ UPX: ${ORIGINAL_SIZE} → ${COMPRESSED_SIZE}${NC}"
	else
		echo -e "${YELLOW}⚠ UPX failed, continuing without compression${NC}"
	fi
else
	echo -e "${YELLOW}⚠ UPX not installed (command not found); skipping binary compression${NC}"
fi

echo -e "${YELLOW}Writing manifest.json (metadata.Build + ldflags)...${NC}"
export M_PACKAGE_ID="$METADATA_PACKAGE_ID" M_NAME="$METADATA_NAME" M_VERSION="$METADATA_VERSION" \
	M_ENTRY_POINT="$ENTRY_POINT" M_TYPE="$PACKAGE_TYPE" M_DESCRIPTION="$METADATA_DESCRIPTION" \
	M_ARCHITECTURE="$GOARCH" M_BUILD_DATE="$BUILD_DATE" M_GIT_COMMIT="$GIT_COMMIT" \
	M_PERMISSIONS="$METADATA_PERMISSIONS"
python3 - <<'PY' >"$BUILD_DIR/manifest.json"
import json, os
from collections import OrderedDict

m = os.environ
perms = json.loads(m.get("M_PERMISSIONS", "[]"))
manifest = OrderedDict(
    [
        ("package_id", m["M_PACKAGE_ID"]),
        ("version", m["M_VERSION"]),
        ("name", m["M_NAME"]),
        ("description", m["M_DESCRIPTION"]),
        ("type", m["M_TYPE"]),
        ("architecture", m["M_ARCHITECTURE"]),
        ("entry_point", m["M_ENTRY_POINT"]),
        ("build_date", m["M_BUILD_DATE"]),
        ("git_commit", m["M_GIT_COMMIT"]),
        ("permissions", perms),
    ]
)
print(json.dumps(manifest, indent=2) + "\n")
PY
echo -e "${GREEN}✓ manifest.json${NC}"

if [[ -d "$SCRIPT_DIR/lib" ]] && [[ -n "$(ls -A "$SCRIPT_DIR/lib" 2>/dev/null)" ]]; then
	echo -e "${YELLOW}Copying extra lib/ ...${NC}"
	cp -r "$SCRIPT_DIR/lib"/* "$BUILD_DIR/lib/" 2>/dev/null || true
	echo -e "${GREEN}✓ Libraries copied${NC}"
fi

ORB_ASSET_DIR="$GO_PKG_ABS/orb"
ORB_PACKED_ROOT=false
if [[ -d "$ORB_ASSET_DIR" ]] && [[ -n "$(ls -A "$ORB_ASSET_DIR" 2>/dev/null)" ]]; then
	echo -e "${YELLOW}Packing orb/ → ORB root ...${NC}"
	cp -a "$ORB_ASSET_DIR"/. "$BUILD_DIR/"
	echo -e "${GREEN}✓ orb/ contents at ORB root ← ${ORB_ASSET_DIR}${NC}"
	ORB_PACKED_ROOT=true
fi

if [[ "$ORB_PACKED_ROOT" != true ]] && [[ -n "${METADATA_WORKDIR:-}" ]]; then
	if [[ "$METADATA_WORKDIR" == /* ]]; then
		echo -e "${RED}metadata.json: data must be relative to the package directory (not an absolute path)${NC}" >&2
		exit 1
	fi
	if [[ "$METADATA_WORKDIR" == *..* ]]; then
		echo -e "${RED}metadata.json: data path must stay under the package directory (no ..)${NC}" >&2
		exit 1
	fi
	WORKDIR_REL="${METADATA_WORKDIR#./}"
	if [[ -z "$WORKDIR_REL" ]]; then
		echo -e "${RED}metadata.json: data cannot be empty or only ./${NC}" >&2
		exit 1
	fi
	WORKDIR_SRC="$GO_PKG_ABS/$WORKDIR_REL"
	if [[ ! -d "$WORKDIR_SRC" ]]; then
		echo -e "${RED}metadata.json: data path is not a directory: ${WORKDIR_SRC}${NC}" >&2
		exit 1
	fi
	echo -e "${YELLOW}Packing metadata data/ → ORB data/ ...${NC}"
	mkdir -p "$BUILD_DIR/data"
	cp -a "$WORKDIR_SRC"/. "$BUILD_DIR/data/"
	echo -e "${GREEN}✓ data/ ← ${METADATA_WORKDIR}${NC}"
fi

echo -e "${YELLOW}Creating ORB package...${NC}"
cd "$BUILD_DIR"
ORB_NAME="${ENTRY_POINT}_v${RELEASE_VERSION}.orb"
printf 'ORB File\n' | zip -r -q -z "$ORB_NAME" . -x"./${ORB_NAME}"

if [[ ! -f "$ORB_NAME" ]]; then
	echo -e "${RED}Error: failed to create ORB${NC}"
	exit 1
fi

mkdir -p "$OUTPUT_DIR"
mv -f "$ORB_NAME" "$OUTPUT_DIR/"
FINAL_ORB="$OUTPUT_DIR/$ORB_NAME"
rm -rf "$BUILD_DIR"

ORB_SIZE="$(du -h "$FINAL_ORB" | cut -f1)"
echo -e "${GREEN}=== Package created ===${NC}"
echo -e "${GREEN}Package: ${FINAL_ORB}${NC}"
echo -e "${GREEN}Size: ${ORB_SIZE}${NC}"
echo ""
echo -e "${YELLOW}ORB contents:${NC}"
echo "  ${ORB_NAME}"
echo "    ├── manifest.json   (metadata.json + build stamps)"
echo "    ├── bin/${ENTRY_POINT}"
echo "    ├── lib/"
if [[ "$ORB_PACKED_ROOT" == true ]]; then
	echo "    ├── …                 ← files from package orb/ at ORB root"
elif [[ -n "${METADATA_WORKDIR:-}" ]]; then
	echo "    ├── data/             ← metadata data: ${METADATA_WORKDIR}"
fi
echo "    └── META-INF/"
