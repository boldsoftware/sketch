#!/bin/bash

# Setup verification script for Tidewave test environment

set -e

echo "🔍 Tidewave Test Environment Setup Check"
echo "========================================"
echo

# Check Erlang
echo "🔍 Checking Erlang..."
if command -v erl &> /dev/null; then
    ERL_VERSION=$(erl -eval 'erlang:display(erlang:system_info(otp_release)), halt().' -noshell 2>/dev/null || echo "unknown")
    echo "✅ Erlang found: OTP $ERL_VERSION"
else
    echo "❌ Erlang not found"
    echo "   Install with: sudo apt install erlang-base erlang-dev erlang-ssl"
    exit 1
fi
echo

# Check Elixir
echo "🔍 Checking Elixir..."
if command -v elixir &> /dev/null; then
    ELIXIR_VERSION=$(elixir --version | grep "Elixir" | cut -d' ' -f2)
    echo "✅ Elixir found: $ELIXIR_VERSION"
else
    echo "❌ Elixir not found"
    echo "   Install with ASDF or download from elixir-lang.org"
    exit 1
fi
echo

# Check Mix
echo "🔍 Checking Mix..."
if command -v mix &> /dev/null; then
    echo "✅ Mix found"
else
    echo "❌ Mix not found (should come with Elixir)"
    exit 1
fi
echo

# Check Hex
echo "🔍 Checking Hex package manager..."
if mix hex.info &> /dev/null; then
    echo "✅ Hex found"
else
    echo "⚠️ Hex not found, installing..."
    mix local.hex --force
    echo "✅ Hex installed"
fi
echo

# Check project dependencies
echo "🔍 Checking project dependencies..."
if [ -d "deps" ] && [ -d "_build" ]; then
    echo "✅ Dependencies appear to be installed"
else
    echo "⚠️ Dependencies not found, installing..."
    mix deps.get
    echo "✅ Dependencies installed"
fi
echo

# Try to compile
echo "🔍 Testing compilation..."
if mix compile --warnings-as-errors; then
    echo "✅ Project compiles successfully"
else
    echo "❌ Compilation failed"
    echo "   Try: mix deps.clean --all && mix deps.get && mix compile"
    exit 1
fi
echo

# Check if Sketch binary exists
echo "🔍 Checking for Sketch binary..."
if [ -f "../sketch" ]; then
    echo "✅ Sketch binary found at ../sketch"
else
    echo "⚠️ Sketch binary not found"
    echo "   Build it with: cd .. && make"
fi
echo

echo "✅ Setup check completed!"
echo "🚀 You can now run:"
echo "   1. mix phx.server              # Start the Phoenix server"
echo "   2. ./test-mcp.sh              # Test with Sketch (in another terminal)"
echo "   3. ./manual-test.sh           # Test MCP protocol manually"
