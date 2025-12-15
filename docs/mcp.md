# MCP Support

Sketch supports the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) to extend its
capabilities with additional tools. Use the `-mcp` flag to configure MCP server
connections:

```sh
# HTTP MCP server
sketch -mcp '{"name": "context7", "type": "http", "url": "https://mcp.context7.com/mcp"}'

# Stdio MCP server
sketch -mcp '{"name": "playwright", "type": "stdio", "command": "npx", "args": ["@playwright/mcp@latest", "--browser=chromium"]}'

# SSE MCP server
sketch -mcp '{"name": "context7-sse", "type": "sse", "url": "https://mcp.context7.com/sse"}'

# Custom local tool with environment variables
sketch -mcp '{"name": "local-tool", "type": "stdio", "command": "my_tool", "args": ["--option", "value"], "env": {"TOKEN": "secret"}}'

# HTTP server with custom headers
sketch -mcp '{"name": "api-server", "type": "http", "url": "http://localhost:8080/mcp", "headers": {"Authorization": "Bearer token"}}'
```

If your MCP server uses the stdio transport, you may need to
[customize your docker base image](docker.md) to support its installation. For example, the
following Dockerfile is useful for Playwright, speeding up the install. (Sketch also has a built-in
browser tool that supports much of Playwright's functionality!)

```dockerfile
FROM ghcr.io/boldsoftware/sketch:latest

# Install Playwright and Chromium browser
RUN npm install -g @playwright/mcp@latest && \
    npx playwright install chromium && \
    npx playwright install-deps chromium
```
