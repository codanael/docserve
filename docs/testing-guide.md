# docserve Testing Guide

## Prerequisites

Build and fetch documentation before testing:

```bash
make build
bin/docserve fetch
```

## Health Checks

```bash
curl http://localhost:8080/healthz
# Expected: ok

curl http://localhost:8080/readyz
# Expected: ready (or "not ready" if no libraries indexed)
```

## MCP Protocol (Streamable HTTP)

All MCP requests go to `POST /mcp` with these headers:

```
Content-Type: application/json
Accept: application/json, text/event-stream
```

After initialization, every request must include the `Mcp-Session-Id` header.

### 1. Start the server

```bash
bin/docserve serve
```

### 2. Initialize a session

```bash
SESSION=$(curl -si -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-03-26",
      "capabilities": {},
      "clientInfo": {"name": "curl-test", "version": "1.0"}
    }
  }' | grep -i mcp-session-id | cut -d' ' -f2 | tr -d '\r')

echo "Session: $SESSION"
```

### 3. Send the initialized notification

```bash
curl -s -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc": "2.0", "method": "notifications/initialized"}'
```

### 4. List available tools

```bash
curl -s -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}' | jq .
```

### 5. List indexed libraries

```bash
curl -s -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "list-libraries",
      "arguments": {}
    }
  }' | jq .
```

### 6. Resolve a library by name

```bash
curl -s -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tools/call",
    "params": {
      "name": "resolve-library",
      "arguments": {"query": "spring"}
    }
  }' | jq .
```

### 7. Search documentation

```bash
curl -s -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{
    "jsonrpc": "2.0",
    "id": 5,
    "method": "tools/call",
    "params": {
      "name": "get-library-docs",
      "arguments": {
        "library": "spring-boot",
        "query": "actuator health endpoint configuration",
        "max_tokens": 3000
      }
    }
  }' | jq .
```

### 8. Close the session

```bash
curl -s -X DELETE http://localhost:8080/mcp \
  -H "Mcp-Session-Id: $SESSION"
```

## MCP Inspector

The [MCP Inspector](https://github.com/modelcontextprotocol/inspector) can be used for interactive testing and protocol validation.

```bash
# List tools
npx -y @modelcontextprotocol/inspector \
  --cli http://localhost:8080/mcp \
  --method tools/list

# Call a tool
npx -y @modelcontextprotocol/inspector \
  --cli http://localhost:8080/mcp \
  --method tools/call \
  --tool-name get-library-docs \
  --tool-arg library=spring-boot \
  --tool-arg query="auto configuration" \
  --tool-arg max_tokens=2000
```

## CLI Commands

```bash
# Fetch all sources
bin/docserve fetch

# Fetch a single source
bin/docserve fetch --source spring-boot

# Force re-fetch (ignore cache)
bin/docserve fetch --force

# List indexed libraries
bin/docserve list

# Search from the command line
bin/docserve search spring-boot "actuator health endpoint"
```
