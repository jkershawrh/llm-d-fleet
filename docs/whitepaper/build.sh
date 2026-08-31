#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

if ! command -v pandoc &>/dev/null; then
    echo "Error: pandoc is required. Install via: brew install pandoc"
    exit 1
fi

echo "Building whitepaper PDF..."
pandoc llm-d-fleet-whitepaper.md \
    -o llm-d-fleet-whitepaper.pdf \
    --pdf-engine=xelatex \
    -V geometry:margin=1in \
    -V fontsize=11pt \
    -V mainfont="Helvetica" \
    --toc \
    --number-sections \
    --highlight-style=tango

echo "Built: llm-d-fleet-whitepaper.pdf"
