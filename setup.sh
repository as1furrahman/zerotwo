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

# Clean any placeholder lines from ~/.bashrc and ~/.zshrc
if [ -f "$HOME/.bashrc" ]; then
  sed -i '/GROQ_API_KEY.*your/d' "$HOME/.bashrc" 2>/dev/null || true
fi
if [ -f "$HOME/.zshrc" ]; then
  sed -i '/GROQ_API_KEY.*your/d' "$HOME/.zshrc" 2>/dev/null || true
fi

# Remove stale config with dummy keys if present
rm -f "$HOME/.config/zerotwo/config.json"

echo "Building ZeroTwo binary..."
go build -trimpath -ldflags="-s -w" -o "$BIN_DIR/zerotwo" cmd/zerotwo/main.go

echo "=============================================="
echo "✓ ZeroTwo installed successfully to $BIN_DIR/zerotwo"
echo "✓ Pre-configured model: openai/gpt-oss-120b"
echo "=============================================="
echo "Testing connection with Groq..."
env -u GROQ_API_KEY -u OPENAI_API_KEY -u ZEROTWO_API_KEY "$BIN_DIR/zerotwo" -m "hi"
