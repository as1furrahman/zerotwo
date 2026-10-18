#!/bin/sh
set -e

# Target binary destination (Termux $PREFIX/bin or user ~/.local/bin)
if [ -n "$PREFIX" ]; then
  BIN_DIR="$PREFIX/bin"
elif [ -w "/usr/local/bin" ]; then
  BIN_DIR="/usr/local/bin"
else
  BIN_DIR="$HOME/.local/bin"
fi
mkdir -p "$BIN_DIR"

echo "Building ZeroTwo binary..."
go build -trimpath -ldflags="-s -w" -o "$BIN_DIR/zerotwo" cmd/zerotwo/main.go

echo "=============================================="
echo "✓ ZeroTwo installed successfully to $BIN_DIR/zerotwo"
echo "✓ Pre-configured model: openai/gpt-oss-120b"
echo "=============================================="
echo "Run 'zerotwo' to start chatting!"
