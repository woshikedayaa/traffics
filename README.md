# Traffics

A simple but powerful port forwarding service

## Overview

Traffics is a lightweight port forwarding tool that supports TCP, UDP, and mixed protocol traffic forwarding. It features simple configuration and high performance.

## Installation

```shell
# Clone the project
git clone --depth=1 --branch=main https://github.com/woshikedayaa/traffics.git

# Build the project
cd traffics/
mkdir -p bin/ && CGO_ENABLED=0 go build -ldflags="-w -s" -v -o bin/traffics .

# Optional: Install to system PATH
install -o root -g root -m 0755 bin/traffics /usr/bin/traffics
```

## Architecture

Traffics uses a Binds (listeners) + Remotes (targets) architecture:

- **Binds**: Local listening configuration, defines ports and protocols to listen on
- **Remotes**: Remote target configuration, defines destination servers for forwarding

Traffic flow: Client → Bind Listener → Remote Target Server

## Usage

### Zero Config Mode (Command Line)

Suitable for simple scenarios, supports multiple forwarding rules:

```shell
# Single forwarding rule
traffics -l "tcp+udp://:5353?remote=dns" -r "dns://1.1.1.1:53"

# Multiple forwarding rules
traffics \
  -l "tcp+udp://:5353?remote=dns" \
  -l "tcp://:8080?remote=web" \
  -r "dns://1.1.1.1:53?timeout=5s" \
  -r "web://192.168.1.100:80?tfo=true"
```

### Configuration File Mode

Suitable for complex scenarios and production environments:

```shell
traffics -c config.json
```

## Configuration File Format

Configuration files support two formats: **URL shorthand** and **complete configuration**, which can be mixed.

### Basic Configuration Structure

```json
{
  "log": {
    "level": "info"
  },
  "binds": [
    // URL shorthand format
    "tcp+udp://:5353?remote=dns&udp_ttl=60s",
    
    // Complete configuration format  
    {
      "listen": "::",
      "port": 8080,
      "network": "tcp",
      "remote": "web"
    }
  ],
  "remotes": [
    // URL shorthand format
    "dns://1.1.1.1:53?strategy=prefer_ipv4&timeout=5s",
    
    // Complete configuration format
    {
      "name": "web",
      "server": "192.168.1.100", 
      "port": 80,
      "timeout": "10s"
    }
  ]
}
```

### URL Format Specification

#### Bind URL Format
```
protocol://[address]:port?param=value&param=value
```

#### Remote URL Format
```
name://server:port?param=value&param=value
```

## Complete Configuration Reference

### Log Configuration

```json
{
  "log": {
    "disable": false,        // Disable logging (default: false)
    "level": "info"         // Log level: trace, debug, info, warn, error, fatal, panic
  }
}
```

### Bind Configuration (Complete Format)

```json
{
  // Required fields
  "listen": "::",           // Listen address
  "port": 9090,            // Listen port
  "remote": "remote_name", // Associated remote service name
  
  // Optional fields
  "name": "bind_name",     // Bind configuration name
  "network": "tcp+udp",    // Network protocol: tcp, udp, tcp+udp (default: tcp+udp)
  "family": "4",           // IP version: 4 or 6
  "interface": "eth0",     // Bind to network interface
  "reuse_addr": false,     // Enable address reuse
  "tfo": false,            // TCP Fast Open
  "mptcp": false,          // Multipath TCP
  "udp_ttl": "60s",        // UDP connection timeout
  "udp_buffer_size": 65507,// UDP buffer size
  "udp_fragment": false    // UDP fragmentation support
}
```

### Remote Configuration (Complete Format)

```json
{
  // Required fields
  "server": "1.1.1.1",    // Target server address
  "port": 53,             // Target server port
  "name": "remote_name",  // Remote service name (corresponds to remote field in bind)
  
  // Optional fields
  "dns": "8.8.8.8",           // Custom DNS server
  "strategy": "prefer_ipv4",  // DNS resolution strategy: prefer_ipv4/prefer_ipv6/ipv4_only/ipv6_only
  "interface": "eth0",         // Outbound network interface
  "timeout": "5s",            // Connection timeout
  "reuse_addr": false,        // Enable address reuse
  "bind_address4": "0.0.0.0", // IPv4 bind address
  "bind_address6": "::",      // IPv6 bind address
  "fw_mark": 0,               // Firewall mark
  "tfo": false,               // TCP Fast Open
  "mptcp": false,             // Multipath TCP
  "udp_fragment": false       // UDP fragmentation support
}
```

### URL Parameters Reference

#### Bind URL Parameters
- `remote`: Associated remote service name (required)
- `name`: Bind configuration name
- `network`: Network protocol (tcp, udp, tcp+udp)
- `family`: IP version (4 or 6)
- `interface`: Bind to network interface
- `reuse_addr`: Enable address reuse (true/false)
- `tfo`: TCP Fast Open (true/false)
- `mptcp`: Multipath TCP (true/false)
- `udp_ttl`: UDP connection timeout (e.g., "60s")
- `udp_buffer_size`: UDP buffer size (integer)
- `udp_fragment`: UDP fragmentation support (true/false)

#### Remote URL Parameters
- `dns`: Custom DNS server
- `strategy`: DNS resolution strategy (prefer_ipv4/prefer_ipv6/ipv4_only/ipv6_only)
- `interface`: Outbound network interface
- `timeout`: Connection timeout (e.g., "5s")
- `reuse_addr`: Enable address reuse (true/false)
- `bind_address4`: IPv4 bind address
- `bind_address6`: IPv6 bind address
- `fw_mark`: Firewall mark (integer)
- `tfo`: TCP Fast Open (true/false)
- `mptcp`: Multipath TCP (true/false)
- `udp_fragment`: UDP fragmentation support (true/false)

## Configuration Examples

### Mixed Format Configuration

```json
{
  "log": {
    "level": "info",
    "disable": false
  },
  "binds": [
    // URL format with advanced UDP settings
    "tcp+udp://[::]:5353?remote=cloudflare_dns&udp_ttl=60s&udp_buffer_size=4096&tfo=true",
    
    // Complete format with interface binding
    {
      "listen": "0.0.0.0",
      "port": 8080,
      "network": "tcp", 
      "remote": "web_server",
      "interface": "eth0",
      "tfo": true,
      "reuse_addr": true
    },
    
    // UDP-only forwarding
    {
      "listen": "::",
      "port": 1194,
      "network": "udp",
      "remote": "vpn_server",
      "udp_ttl": "300s",
      "udp_fragment": true
    }
  ],
  "remotes": [
    // URL format with DNS strategy
    "cloudflare_dns://1.1.1.1:53?strategy=prefer_ipv4&timeout=10s&tfo=true",
    
    // Complete format with custom DNS and binding
    {
      "name": "web_server",
      "server": "backend.example.com",
      "port": 80,
      "timeout": "15s",
      "strategy": "prefer_ipv4",
      "dns": "8.8.8.8",
      "interface": "eth0",
      "bind_address4": "192.168.1.10"
    },
    
    // VPN server with fragmentation support
    {
      "name": "vpn_server",
      "server": "vpn.example.com",
      "port": 1194,
      "timeout": "30s",
      "strategy": "prefer_ipv6",
      "udp_fragment": true,
      "fw_mark": 100
    }
  ]
}
```

### Command Line Examples

```shell
# DNS forwarding with UDP optimization
traffics -l "udp://:5353?remote=dns&udp_ttl=120s" -r "dns://1.1.1.1:53?strategy=ipv4_only"

# Web server with TCP Fast Open
traffics -l "tcp://0.0.0.0:8080?remote=web&tfo=true" -r "web://192.168.1.100:80?tfo=true&timeout=10s"

# Game server with mixed protocols
traffics -l "tcp+udp://0.0.0.0:25565?remote=game" -r "game://game.example.com:25565?timeout=15s&strategy=prefer_ipv4"

# Multiple services
traffics \
  -l "tcp+udp://:53?remote=dns&udp_ttl=60s" \
  -l "tcp://:80?remote=web&tfo=true" \
  -l "udp://:1194?remote=vpn&udp_ttl=300s" \
  -r "dns://1.1.1.1:53?strategy=prefer_ipv4" \
  -r "web://backend:8080?timeout=10s&tfo=true" \
  -r "vpn://vpn.company.com:1194?udp_fragment=true"
```

## Important Notes

1. In `tcp+udp` mode, both TCP and UDP traffic will be forwarded to the same remote service
2. The `remote` field in binds must match the `name` field in remotes configuration
3. Configuration files are recommended for production environments for easier management
4. UDP forwarding supports session persistence controlled by `udp_ttl` timeout setting
5. Both URL and complete configuration formats can be mixed in the same configuration file

## Acknowledgments

This project uses the following excellent open-source libraries:

- **[tfo-go](https://github.com/metacubex/tfo-go)** - TCP Fast Open implementation for Go
- **[dns](https://github.com/miekg/dns)** - DNS library in Go
- **[sing](https://github.com/sagernet/sing)** - Universal proxy platform

We are grateful to all contributors and maintainers of these projects for their valuable work.