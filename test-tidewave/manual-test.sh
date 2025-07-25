#!/bin/bash

# Manual MCP protocol test
# This script tests the MCP protocol directly without Sketch

set -e

TIDEWAVE_URL="http://localhost:4000/tidewave/mcp"

echo "🌊 Manual Tidewave MCP Protocol Test"
echo "===================================="
echo

# Check if server is running
echo "📡 Checking if Tidewave server is running..."
if ! curl -s --connect-timeout 5 "$TIDEWAVE_URL" > /dev/null 2>&1; then
    echo "❌ Tidewave server not responding at $TIDEWAVE_URL"
    echo "   Please start the Phoenix server first: mix phx.server"
    exit 1
fi
echo "✅ Tidewave server is running"
echo

# Step 1: Get session ID
echo "🎫 Step 1: Getting session ID from SSE endpoint..."
SSE_OUTPUT=$(timeout 3 curl -s "$TIDEWAVE_URL" || echo "timeout")
SESSION_ID=$(echo "$SSE_OUTPUT" | grep -o 'sessionId=[^"]*' | cut -d'=' -f2 | head -1)

if [ -z "$SESSION_ID" ]; then
    echo "❌ Failed to get session ID"
    echo "   SSE output: $SSE_OUTPUT"
    exit 1
fi

echo "✅ Got session ID: $SESSION_ID"
echo

# Step 2: Initialize session
echo "🔄 Step 2: Initializing MCP session..."
INIT_RESPONSE=$(curl -s -X POST "$TIDEWAVE_URL/message?sessionId=$SESSION_ID" \
    -H "Content-Type: application/json" \
    -d '{
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {
                "name": "manual-test",
                "version": "1.0"
            }
        }
    }')

echo "Initialize response: $INIT_RESPONSE"
echo

# Step 3: Send initialized notification
echo "📤 Step 3: Sending initialized notification..."
INITIALIZED_RESPONSE=$(curl -s -X POST "$TIDEWAVE_URL/message?sessionId=$SESSION_ID" \
    -H "Content-Type: application/json" \
    -d '{
        "jsonrpc": "2.0",
        "method": "notifications/initialized"
    }')

echo "Initialized response: $INITIALIZED_RESPONSE"
echo

# Step 4: List available tools
echo "🔧 Step 4: Listing available tools..."
TOOLS_RESPONSE=$(curl -s -X POST "$TIDEWAVE_URL/message?sessionId=$SESSION_ID" \
    -H "Content-Type: application/json" \
    -d '{
        "jsonrpc": "2.0",
        "id": 2,
        "method": "tools/list"
    }')

echo "Tools response: $TOOLS_RESPONSE"
echo

# Step 5: Call project_eval tool
echo "🧮 Step 5: Testing project_eval with '1 + 1'..."
EVAL_RESPONSE=$(curl -s -X POST "$TIDEWAVE_URL/message?sessionId=$SESSION_ID" \
    -H "Content-Type: application/json" \
    -d '{
        "jsonrpc": "2.0",
        "id": 3,
        "method": "tools/call",
        "params": {
            "name": "project_eval",
            "arguments": {
                "code": "1 + 1"
            }
        }
    }')

echo "Eval response: $EVAL_RESPONSE"
echo

echo "✅ Manual test completed!"
echo "   Check the responses above for any errors."
echo "   If you see {\"status\":\"ok\"} responses, the MCP protocol is working."
