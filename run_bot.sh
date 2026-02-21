#!/usr/bin/env bash
# Run TibiaBot v2
# Make sure to run:  uv sync  first to install dependencies.

set -e
cd "$(dirname "$0")"

uv run python -m bot.main "$@"
