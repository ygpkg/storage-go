#!/usr/bin/env bash
set -euo pipefail

RESTORE=$(pwd)
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DRIVER="${1:-local}"
DRIVER_UPPER=$(echo "$DRIVER" | tr '[:lower:]' '[:upper:]')

if [ -f "$ROOT/.env.test" ]; then
  set -a
  source "$ROOT/.env.test"
  set +a
fi

setup() {
  local prefix="$1"
  local required="${2:-endpoint access_key secret_key bucket}"
  local region_required="${3:-0}"

  for field in $required; do
    local var="${prefix}_$(echo "$field" | tr '[:lower:]' '[:upper:]')"
    [ -n "${!var:-}" ] || { echo "ERROR: ${var} must be set in .env.test for driver=$DRIVER" >&2; exit 1; }
  done

  export STORAGE_DRIVER="$DRIVER"

  export STORAGE_ENDPOINT="${!prefix_var:-}" 2>/dev/null || true
  export STORAGE_ENDPOINT=$(eval echo \$"${prefix}_ENDPOINT")
  export STORAGE_REGION=$(eval echo \$"${prefix}_REGION")
  export STORAGE_ACCESS_KEY=$(eval echo \$"${prefix}_ACCESS_KEY")
  export STORAGE_SECRET_KEY=$(eval echo \$"${prefix}_SECRET_KEY")
  export STORAGE_BUCKET=$(eval echo \$"${prefix}_BUCKET")
  export STORAGE_BASE_URL=$(eval echo \$"${prefix}_BASE_URL")
  export STORAGE_BASE_DIR=$(eval echo \$"${prefix}_BASE_DIR")
}

case "$DRIVER" in
  local)     setup LOCAL "bucket base_dir" "0" ;;
  minio)     setup MINIO "endpoint access_key secret_key bucket" "0" ;;
  seaweedfs) setup SEAWEEDFS "endpoint access_key secret_key bucket" "0" ;;
  cos)       setup COS "endpoint region access_key secret_key bucket" "0" ;;
  *)
    echo "Usage: $0 {local|minio|cos|seaweedfs}"
    exit 1
    ;;
esac

echo "=== Testing with driver=$STORAGE_DRIVER bucket=$STORAGE_BUCKET ==="
cd "$ROOT" && go test -v -count=1 ./...

cd "$RESTORE" 2>/dev/null || true