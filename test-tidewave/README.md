# Tidewave MCP Server Test Environment

This directory contains a test Phoenix project configured with Tidewave for testing MCP integration with Sketch.

## Purpose

This test environment demonstrates and validates the fix for "Could not find session" errors when using Sketch with Tidewave's MCP server. It includes:

- A patched version of Tidewave with increased initialization timeout (120s)
- A working Phoenix project setup
- Test scripts to validate the integration

## Prerequisites

You need Erlang/OTP and Elixir installed. The easiest way is using ASDF version manager.

### Option 1: Install with ASDF (Recommended)

```bash
# Install ASDF if not already installed
git clone https://github.com/asdf-vm/asdf.git ~/.asdf --branch v0.14.0
echo '. ~/.asdf/asdf.sh' >> ~/.bashrc
echo '. ~/.asdf/completions/asdf.bash' >> ~/.bashrc
source ~/.bashrc

# Add Erlang and Elixir plugins
asdf plugin add erlang https://github.com/asdf-vm/asdf-erlang.git
asdf plugin add elixir https://github.com/asdf-vm/asdf-elixir.git

# Install the versions used in this project
asdf install erlang 26.2.5.2
asdf install elixir 1.17.3-otp-26

# Set them as local versions for this project
cd test-tidewave
asdf local erlang 26.2.5.2
asdf local elixir 1.17.3-otp-26
```

### Option 2: Install with Package Manager

**Ubuntu/Debian:**
```bash
sudo apt update
sudo apt install erlang-base erlang-dev erlang-ssl erlang-crypto erlang-inets
wget https://github.com/elixir-lang/elixir/releases/download/v1.17.3/elixir-otp-26.zip
unzip elixir-otp-26.zip -d /usr/local/elixir
export PATH="/usr/local/elixir/bin:$PATH"
```

**macOS:**
```bash
brew install erlang elixir
```

## Setup Instructions

1. **Navigate to the test directory:**
   ```bash
   cd test-tidewave
   ```

2. **Install Hex package manager:**
   ```bash
   mix local.hex --force
   ```

3. **Install dependencies:**
   ```bash
   mix deps.get
   ```

4. **Compile the project:**
   ```bash
   mix compile
   ```

## Running the Test

### Start the Phoenix Server

```bash
# Start the Phoenix server with Tidewave
mix phx.server
```

The server will start on `http://localhost:4000` and the Tidewave MCP endpoint will be available at `http://localhost:4000/tidewave/mcp`.

### Test with Sketch

In another terminal, test the MCP integration:

```bash
# Navigate back to the main Sketch directory
cd ..

# Test with the updated timeout (should work)
ANTHROPIC_API_KEY="your-api-key" ./sketch -skaband-addr= -unsafe -one-shot \
  -mcp '{"name": "tidewave", "type": "sse", "url": "http://localhost:4000/tidewave/mcp"}' \
  -prompt 'Use tidewave_project_eval to evaluate: 1 + 1'
```

### Manual Testing

You can also test the MCP protocol manually:

```bash
# Get a session ID
curl -s --max-time 3 http://localhost:4000/tidewave/mcp

# Extract session ID from the output (e.g., sessionId=abc123...)
SESSION_ID="your-session-id-here"

# Send initialize message
curl -s -X POST "http://localhost:4000/tidewave/mcp/message?sessionId=$SESSION_ID" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}}'

# Send initialized notification
curl -s -X POST "http://localhost:4000/tidewave/mcp/message?sessionId=$SESSION_ID" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "method": "notifications/initialized"}'

# Call project_eval tool
curl -s -X POST "http://localhost:4000/tidewave/mcp/message?sessionId=$SESSION_ID" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "project_eval", "arguments": {"code": "1 + 1"}}}'
```

## What's Different

This test environment includes a **patched version** of Tidewave with the following fix:

- **File:** `tidewave_patched/lib/tidewave/mcp/connection.ex`
- **Change:** Increased `@init_timeout` from 30,000ms to 120,000ms
- **Purpose:** Prevents "Could not find session" errors by giving Sketch's MCP client enough time to complete initialization

The project's `mix.exs` is configured to use the local patched version instead of the hex package.

## Available Tools

When properly connected, Tidewave provides these MCP tools:
- `tidewave_project_eval` - Evaluate Elixir code in project context
- `tidewave_get_logs` - Get application logs
- `tidewave_get_docs` - Get documentation for modules/functions
- `tidewave_get_source_location` - Get source location for references
- `tidewave_shell_eval` - Execute shell commands (if enabled)

## Troubleshooting

### Port Already in Use
If port 4000 is busy:
```bash
PORT=4001 mix phx.server
# Then update the URL in your Sketch command to use port 4001
```

### Locale Issues
If you see UTF-8 warnings:
```bash
export ELIXIR_ERL_OPTIONS="+fnu"
```

### Dependencies Issues
```bash
# Clean and reinstall
mix deps.clean --all
mix deps.get
mix compile
```

## Verifying the Fix

**Before the fix:** Sketch would get "Could not find session" errors after 30 seconds

**After the fix:** Sketch successfully connects and can execute tools like `tidewave_project_eval`

The key difference is that the patched Tidewave gives the MCP client 120 seconds instead of 30 seconds to complete the initialization handshake, matching Sketch's updated connection timeout.
