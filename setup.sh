#!/bin/sh
set -e

# Target binary destination (Termux $PREFIX/bin or system /usr/local/bin)
BIN_DIR="${PREFIX:-/usr/local}/bin"
mkdir -p "$BIN_DIR"

echo "Building ZeroTwo static binary..."
go build -trimpath -ldflags="-s -w" -o "$BIN_DIR/zerotwo" cmd/zerotwo/main.go

echo "=============================================="
echo "✓ ZeroTwo installed successfully to $BIN_DIR/zerotwo"
echo "✓ Pre-configured model: openai/gpt-oss-120b"
echo "=============================================="
echo "Testing connection with Groq..."
"$BIN_DIR/zerotwo" -m "Hello ZeroTwo!"
