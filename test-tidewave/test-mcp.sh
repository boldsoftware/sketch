#!/bin/bash

# Test script for Sketch + Tidewave MCP integration
# Usage: ./test-mcp.sh [your-anthropic-api-key]

set -e

API_KEY="${1:-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}"
SKETCH_PATH="../sketch"
TIDEWAVE_URL="http://localhost:4000/tidewave/mcp"

echo "🌊 Testing Sketch + Tidewave MCP Integration"
echo "============================================"
echo

# Check if Sketch binary exists
if [ ! -f "$SKETCH_PATH" ]; then
    echo "❌ Sketch binary not found at $SKETCH_PATH"
    echo "   Please build Sketch first: cd .. && make"
    exit 1
fi

# Check if Phoenix server is running
echo "📡 Checking if Tidewave server is running..."
if ! curl -s --connect-timeout 5 "$TIDEWAVE_URL" > /dev/null 2>&1; then
    echo "❌ Tidewave server not responding at $TIDEWAVE_URL"
    echo "   Please start the Phoenix server first: mix phx.server"
    exit 1
fi
echo "✅ Tidewave server is running"
echo

# Test 1: Simple arithmetic
echo "🧮 Test 1: Simple arithmetic (1 + 1)"
echo "====================================="
"$API_KEY" "$SKETCH_PATH" -skaband-addr= -unsafe -one-shot \
    -mcp "{\"name\": \"tidewave\", \"type\": \"sse\", \"url\": \"$TIDEWAVE_URL\"}" \
    -prompt 'Use tidewave_project_eval to evaluate: 1 + 1'
echo
echo

# Test 2: String operations
echo "📝 Test 2: String operations"
echo "============================"
ANTHROPIC_API_KEY="$API_KEY" "$SKETCH_PATH" -skaband-addr= -unsafe -one-shot \
    -mcp "{\"name\": \"tidewave\", \"type\": \"sse\", \"url\": \"$TIDEWAVE_URL\"}" \
    -prompt 'Use tidewave_project_eval to evaluate: "Hello, " <> "World!"'
echo
echo

# Test 3: List functions
echo "📋 Test 3: List operations"
echo "========================="
ANTHROPIC_API_KEY="$API_KEY" "$SKETCH_PATH" -skaband-addr= -unsafe -one-shot \
    -mcp "{\"name\": \"tidewave\", \"type\": \"sse\", \"url\": \"$TIDEWAVE_URL\"}" \
    -prompt 'Use tidewave_project_eval to evaluate: Enum.sum([1, 2, 3, 4, 5])'
echo
echo

echo "✅ All tests completed!"
echo "   If you see results above (not errors), the integration is working."
