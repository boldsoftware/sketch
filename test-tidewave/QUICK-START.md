# Quick Start Guide

## 1. Install Erlang + Elixir

**Option A: ASDF (Recommended)**
```bash
make install-asdf
source ~/.bashrc  # Restart shell
```

**Option B: Ubuntu/Debian**
```bash
sudo apt update
sudo apt install erlang-base erlang-dev erlang-ssl erlang-crypto erlang-inets
# Then install Elixir 1.17+ from elixir-lang.org
```

## 2. Setup Project

```bash
make setup    # Install deps and compile
make check    # Verify everything works
```

## 3. Test Integration

**Terminal 1:**
```bash
make server   # Start Phoenix server
```

**Terminal 2:**
```bash
# Build Sketch first if needed
cd .. && make && cd test-tidewave

# Run tests
make test     # Test with Sketch
# OR
make manual   # Test MCP protocol directly
```

## Expected Results

✅ **Success:** Sketch executes `tidewave_project_eval` and returns results  
❌ **Failure:** "Could not find session" or timeout errors

## Key Files

- `README.md` - Full documentation
- `tidewave_patched/` - Patched Tidewave with 120s timeout
- `mix.exs` - Configured to use patched version
- `test-mcp.sh` - Automated Sketch integration test
- `manual-test.sh` - Manual MCP protocol test
- `setup-check.sh` - Environment verification

## Troubleshooting

**Port 4000 busy:** `PORT=4001 make server`  
**Locale issues:** `export ELIXIR_ERL_OPTIONS="+fnu"`  
**Deps issues:** `make clean && make setup`
