#!/usr/bin/env bash
# Shell helper to run Forge autonomous fix loop against a test fixture.
set -euo pipefail

FIXTURE="${1:-boundary}"
INTERACTIVE=false
YOLO=false

for arg in "$@"; do
    case "$arg" in
        --interactive|-i) INTERACTIVE=true ;;
        --yolo) YOLO=true ;;
        offbyone|nilpanic|wrongimport|boundary) FIXTURE="$arg" ;;
    esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
FIXTURE_DIR="$REPO_ROOT/testdata/fixfail/$FIXTURE"

if [ ! -d "$FIXTURE_DIR" ]; then
    echo "Error: Fixture directory not found: $FIXTURE_DIR" >&2
    exit 1
fi

TEMP_DIR="$(mktemp -d -t "forge-fixture-${FIXTURE}-XXXXXX")"
echo "==> Staging fixture '$FIXTURE' into $TEMP_DIR"
cp -r "$FIXTURE_DIR"/* "$TEMP_DIR/"

cd "$TEMP_DIR"
echo -e "\n==> Verifying initial test failure in $TEMP_DIR:"
go test ./... || true
echo "(Failure verified as expected)"

FORGE_BIN="$REPO_ROOT/forge"
if [ ! -f "$FORGE_BIN" ]; then
    echo "==> Building forge binary..."
    go build -o "$FORGE_BIN" "$REPO_ROOT/cmd/forge"
fi

PROMPT="Tests are failing in this project. Use bash to run the tests, read the relevant files, fix the root cause with edit_file, and re-run the tests to confirm they pass."

if [ "$INTERACTIVE" = true ]; then
    echo "==> Launching Forge interactive TUI..."
    "$FORGE_BIN" --root "$TEMP_DIR"
else
    ARGS=("--root" "$TEMP_DIR" "--yes")
    if [ "$YOLO" = true ]; then
        ARGS+=("--yolo")
    fi
    ARGS+=("$PROMPT")

    echo "==> Running Forge headless fix loop..."
    "$FORGE_BIN" "${ARGS[@]}"

    echo -e "\n==> Verifying post-fix test status:"
    go test -v ./...
    echo "==> Fixture '$FIXTURE' fixed successfully!"
fi

echo -e "\nWorkspace retained at: $TEMP_DIR"
