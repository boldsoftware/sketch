#!/bin/bash

# This script builds and runs the Kifaru application with environment variables
# loaded from the .env file.

# Exit immediately if a command exits with a non-zero status.
set -e

echo "Loading environment variables from .env..."
if [ ! -f .env ]; then
    echo "Error: .env file not found. Please create one from .env.example."
    exit 1
fi
set -a # Automatically export all variables
source .env
set +a # Stop automatically exporting
echo "Environment variables loaded."

echo "Building Kifaru binary..."
go build -tags innie -o kifaru ./cmd/sketch
echo "Build complete."

echo "Starting Kifaru..."
# Run kifaru with all arguments passed to this script
./kifaru "$@"
