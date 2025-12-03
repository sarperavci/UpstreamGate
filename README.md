# UpstreamGate

A dynamic HTTP CONNECT proxy server that allows real-time switching of upstream proxies per user via a simple REST API.

## Description / Overview

UpstreamGate is a lightweight Go-based proxy gateway that routes traffic through configurable upstream proxies. It supports per-user upstream configuration, allowing you to dynamically change which proxy server handles a user's traffic without restarting the service. Perfect for scenarios requiring flexible proxy routing, multi-tenant proxy setups, or testing different proxy configurations on the fly.

## Demo

```
┌──────────┐      ┌───────────────┐      ┌──────────────┐      ┌────────────┐
│  Client  │ ───► │ UpstreamGate  │ ───► │   Upstream   │ ───► │   Target   │
│          │      │   (:8090)     │      │    Proxy     │      │   Server   │
└──────────┘      └───────────────┘      └──────────────┘      └────────────┘
                         ▲
                         │ POST /upstream
                         │ {"user":"...", "upstream":"socks5://..."}
                  ┌──────┴──────┐
                  │  Admin API  │
                  └─────────────┘
```

## Installation

### Prerequisites
- Go 1.18 or higher

### Build from source

```bash
# Clone the repository
git clone https://github.com/sarperavci/UpstreamGate.git
cd UpstreamGate

# Install dependencies
go mod tidy

# Build the binary
go build -o upstreamgate main.go

# Run the server
./upstreamgate
```

### Quick start with Go

```bash
go run main.go
```

The proxy server will start on port `8090`.

## Usage

### Starting the Proxy

```bash
./upstreamgate
# Output: proxy listening on :8090
```

### Setting an Upstream Proxy for a User

Use the `/upstream` endpoint to configure which upstream proxy a user should use:

```bash
# Set SOCKS5 upstream for user "alice"
curl -X POST http://localhost:8090/upstream \
  -H "Content-Type: application/json" \
  -d '{"user": "alice", "password": "secret", "upstream": "socks5://proxy.example.com:1080"}'

# Set HTTP upstream with authentication
curl -X POST http://localhost:8090/upstream \
  -H "Content-Type: application/json" \
  -d '{"user": "bob", "password": "pass123", "upstream": "http://user:pass@proxy.example.com:8080"}'

# Set direct connection (no upstream proxy)
curl -X POST http://localhost:8090/upstream \
  -H "Content-Type: application/json" \
  -d '{"user": "charlie", "password": "mypass", "upstream": "direct://"}'
```

### Using the Proxy

Connect through the proxy using HTTP CONNECT method with Basic authentication:

```bash
# Using curl with the proxy
curl -x http://alice:secret@localhost:8090 https://api.example.com

# Using environment variables
export https_proxy=http://alice:secret@localhost:8090
curl https://api.example.com
```

### Switching Upstreams on the Fly

When you update a user's upstream configuration, all existing connections for that user are automatically closed, forcing them to reconnect through the new upstream:

```bash
# Switch alice to a different upstream
curl -X POST http://localhost:8090/upstream \
  -H "Content-Type: application/json" \
  -d '{"user": "alice", "password": "secret", "upstream": "socks5://newproxy.example.com:1080"}'
```

## API Reference

### POST /upstream

Configure the upstream proxy for a user.

**Request Body:**
```json
{
  "user": "username",
  "password": "password",
  "upstream": "socks5://host:port"
}
```

**Supported Upstream Schemes:**
| Scheme | Example | Description |
|--------|---------|-------------|
| `socks5` | `socks5://host:1080` | SOCKS5 proxy |
| `socks5` | `socks5://user:pass@host:1080` | SOCKS5 with auth |
| `http` | `http://host:8080` | HTTP CONNECT proxy |
| `http` | `http://user:pass@host:8080` | HTTP proxy with auth |
| `direct` | `direct://` | Direct connection (no proxy) |

**Response:**
- `204 No Content` - Success
- `400 Bad Request` - Invalid JSON or upstream URL
- `405 Method Not Allowed` - Non-POST request

## License

MIT License - feel free to use this project for any purpose.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
