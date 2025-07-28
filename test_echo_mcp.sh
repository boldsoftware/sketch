#!/bin/bash

# Test script for the echo MCP tool
# This script tests various aspects of the echo tool functionality

set -e

echo "=== MCP Echo Tool Test Suite ==="
echo

# Build the mcp-tool if it doesn't exist
if [ ! -f "/app/bin/mcp-tool" ]; then
    echo "Building mcp-tool..."
    cd /app && go build -o bin/mcp-tool cmd/mcp-tool/mcp-tool.go
fi

# Test server configuration
MCP_CONFIG='{"name": "test", "type": "stdio", "command": "python3", "args": ["test-mcp-server.py"]}'

echo "1. Testing tool discovery..."
./bin/mcp-tool discover -mcp "$MCP_CONFIG" | grep -q "echo" && echo "✓ Echo tool discovered" || echo "✗ Echo tool not found"
echo

echo "2. Testing basic echo functionality..."
result=$(./bin/mcp-tool call -mcp "$MCP_CONFIG" echo '{"message": "Hello, World!"}')
if echo "$result" | grep -q "Echo: Hello, World!"; then
    echo "✓ Basic echo test passed"
else
    echo "✗ Basic echo test failed"
    echo "Result: $result"
fi
echo

echo "3. Testing empty message echo..."
result=$(./bin/mcp-tool call -mcp "$MCP_CONFIG" echo '{"message": ""}')
if echo "$result" | grep -q "Echo: "; then
    echo "✓ Empty message test passed"
else
    echo "✗ Empty message test failed"
    echo "Result: $result"
fi
echo

echo "4. Testing echo with special characters..."
result=$(./bin/mcp-tool call -mcp "$MCP_CONFIG" echo '{"message": "Test with \"quotes\" and newlines\n!"}')
if echo "$result" | grep -q "Echo: Test with"; then
    echo "✓ Special characters test passed"
else
    echo "✗ Special characters test failed"
    echo "Result: $result"
fi
echo

echo "5. Testing echo with long message..."
long_message="This is a very long message that tests the echo tool's ability to handle larger text inputs without any issues or truncation."
result=$(./bin/mcp-tool call -mcp "$MCP_CONFIG" echo "{\"message\": \"$long_message\"}")
if echo "$result" | grep -q "Echo: $long_message"; then
    echo "✓ Long message test passed"
else
    echo "✗ Long message test failed"
    echo "Result: $result"
fi
echo

echo "6. Testing verbose mode..."
result=$(./bin/mcp-tool call -v -mcp "$MCP_CONFIG" echo '{"message": "Verbose test"}' 2>&1)
if echo "$result" | grep -q "Calling tool \"echo\" with arguments"; then
    echo "✓ Verbose mode test passed"
else
    echo "✗ Verbose mode test failed"
    echo "Result: $result"
fi
echo

echo "=== All tests completed! ==="
