# Test Environment Contents

This directory contains a complete test environment for validating the Sketch + Tidewave MCP integration fix.

## Project Structure

```
test-tidewave/
├── README.md              # Full setup and usage documentation
├── QUICK-START.md         # Quick reference guide
├── CONTENTS.md           # This file - describes what's included
├── Makefile              # Convenient commands for setup and testing
│
├── mix.exs               # Phoenix project configuration (uses patched Tidewave)
├── mix.lock              # Locked dependency versions
├── config/               # Phoenix configuration files
├── lib/                  # Phoenix application code
├── priv/                 # Phoenix static assets
├── test/                 # Phoenix test files
├── deps/                 # Elixir dependencies (created after mix deps.get)
├── _build/               # Compiled Elixir code (created after mix compile)
│
├── tidewave_patched/     # ⭐ PATCHED VERSION of Tidewave
│   └── lib/tidewave/mcp/
│       ├── connection.ex # ✅ FIXED: @init_timeout 30s → 120s
│       └── sse.ex        # Session management implementation
│
├── setup-check.sh        # 🔧 Verify environment setup
├── test-mcp.sh          # 🧪 Test Sketch + Tidewave integration
├── manual-test.sh       # 🛠️  Manual MCP protocol testing
│
└── simple_eval_test.exs  # Standalone Elixir evaluation test
```

## Key Fix Applied

**File:** `tidewave_patched/lib/tidewave/mcp/connection.ex`  
**Change:** Line 32: `@init_timeout 30_000` → `@init_timeout 120_000`  
**Purpose:** Prevents "Could not find session" errors by matching Sketch's new 120s connection timeout

## Test Scripts

1. **`setup-check.sh`** - Verifies Erlang/Elixir installation and project compilation
2. **`test-mcp.sh`** - Runs automated tests using Sketch with different Elixir expressions
3. **`manual-test.sh`** - Tests the MCP protocol directly with curl commands

## Phoenix Project Features

- **Framework:** Phoenix 1.7.21 with Bandit web server
- **Tidewave:** Patched version with extended timeout
- **MCP Endpoint:** `http://localhost:4000/tidewave/mcp`
- **Available Tools:** `tidewave_project_eval`, `tidewave_get_logs`, etc.

## Usage Scenarios

1. **Validate the fix:** Confirm Sketch no longer gets "Could not find session" errors
2. **Reproduce the bug:** Revert the patch and see the original 30s timeout issue
3. **Development testing:** Test other MCP integrations or Tidewave features
4. **Documentation:** Demonstrate proper MCP server setup and configuration

## Dependencies

- **Erlang/OTP:** 26.2.5.2 (minimum 25+)
- **Elixir:** 1.17.3 (minimum 1.14+)
- **Phoenix:** 1.7.21
- **Tidewave:** 0.2.0 (patched locally)

The environment is completely self-contained and doesn't require external services or databases.
