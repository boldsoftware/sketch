# Unsafe Mode Memory Leak Fixes

## 🚨 **Issue Identified**

When running sketch in **unsafe mode** (`-unsafe` flag) without ska-band integration (`-skaband-addr=""`), the web UI causes severe memory leaks that can crash the system by consuming all available RAM.

## 🔍 **Root Cause Analysis**

### **Unsafe Mode Flag**
```bash
# The problematic command that causes memory leaks:
sketch -unsafe -skaband-addr="" 
```

- **`-unsafe`**: Runs sketch directly on the host without Docker container
- **`-skaband-addr=""`**: Disables sketch.dev integration (ska-band service)
- **Web UI**: Accessing the web interface in browser triggers memory leaks

### **Memory Leak Sources**

1. **SSE Stream Handler (`/stream` endpoint)**:
   - Created channels with 1000-item buffers per client
   - Multiple goroutines per connection without proper cleanup
   - Message iterators holding references to entire message history
   - No connection limits allowing unlimited concurrent connections

2. **Terminal Sessions**:
   - Each terminal created PTY processes with 4096-buffer channels
   - Event channels accumulated without proper cleanup
   - No limits on concurrent terminal sessions

3. **Iterator Accumulation**:
   - Each SSE client created message and state iterators
   - Iterators held references to large amounts of data
   - Multiple clients multiplied memory usage exponentially

## 🔧 **Fixes Implemented**

### **1. Connection Limits**
```go
// Added connection tracking and limits
maxSSEConnections:   10,  // Limit concurrent SSE connections
maxTerminalSessions: 5,   // Limit concurrent terminal sessions
```

### **2. Reduced Channel Buffer Sizes**
```go
// Before: Massive buffers causing memory leaks
messageChan := make(chan *loop.AgentMessage, 1000)
events := make(chan []byte, 4096)

// After: Reasonable buffers with overflow protection
messageChan := make(chan *loop.AgentMessage, 10)
events := make(chan []byte, 100)
```

### **3. Connection Timeouts**
```go
// Added 30-minute timeout for SSE connections
ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
```

### **4. Proper Cleanup**
- Added connection tracking with unique IDs
- Ensured proper cleanup when clients disconnect
- Added overflow protection (drop messages instead of blocking)
- Improved goroutine cancellation handling

### **5. Memory Monitoring**
```bash
# New endpoint to monitor memory usage:
curl http://localhost:PORT/debug/memory
```

Returns:
```json
{
  "memory": {
    "alloc_mb": 45.2,
    "heap_alloc_mb": 45.2,
    "sys_mb": 78.5
  },
  "connections": {
    "sse_connections": 2,
    "max_sse_connections": 10,
    "terminal_sessions": 1,
    "max_terminal_sessions": 5
  },
  "goroutines": 25
}
```

## 🎯 **How to Use Safely**

### **Before (Dangerous)**
```bash
# This would cause memory leaks and system crashes:
sketch -unsafe -skaband-addr=""
# Then opening web UI in browser
```

### **After (Safe)**
```bash
# Same command, but now with memory leak protections:
sketch -unsafe -skaband-addr=""
# Web UI now has:
# - Connection limits (max 10 SSE, max 5 terminals)
# - Reduced buffer sizes
# - Automatic cleanup
# - Memory monitoring
```

## 📊 **Monitoring Memory Usage**

### **Check Memory Stats**
```bash
# Monitor memory usage in real-time:
curl http://localhost:PORT/debug/memory | jq

# Watch for these warning signs:
# - alloc_mb growing continuously
# - sse_connections near max (10)
# - terminal_sessions near max (5)
# - goroutines growing without bound
```

### **Debug Endpoints**
```bash
# Memory statistics
curl http://localhost:PORT/debug/memory

# Full debug info
curl http://localhost:PORT/debug/

# Go profiling
curl http://localhost:PORT/debug/pprof/heap
```

## ⚠️ **Important Notes**

1. **Connection Limits**: The web UI will now show \"Too many connections\" if limits are exceeded
2. **Buffer Overflow**: Messages may be dropped if clients are too slow (logged as warnings)
3. **Timeouts**: SSE connections automatically close after 30 minutes of inactivity
4. **Monitoring**: Use `/debug/memory` to track resource usage

## 🔄 **Ska-Band Service**

The ska-band service is a separate component that provides:
- Session management and history
- Multi-user collaboration
- Cloud integration
- Enhanced security

When `skaband-addr` is empty, sketch runs in standalone mode without these features, which is when the memory leaks occurred.

## ✅ **Verification**

To verify the fixes work:

1. **Start sketch in unsafe mode**:
   ```bash
   sketch -unsafe -skaband-addr=""
   ```

2. **Open web UI in browser** (should no longer cause memory leaks)

3. **Monitor memory usage**:
   ```bash
   watch -n 5 'curl -s http://localhost:PORT/debug/memory | jq .memory.alloc_mb'
   ```

4. **Test connection limits**:
   - Open multiple browser tabs
   - Should see \"Too many connections\" after 10 SSE connections

The memory usage should now remain stable instead of growing unbounded.