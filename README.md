# Traffics

A simple but powerful port forwarding service

## Overview

Traffics is a lightweight port forwarding tool that supports TCP, UDP, and mixed protocol traffic forwarding. It features simple configuration and high performance.

## Installation

### 1. Clone the project
```shell
git clone --depth=1 --branch=main https://github.com/woshikedayaa/traffics.git
```

### 2. Change to project directory
```shell
cd traffics/
```

### 3. Build
```shell
mkdir -p bin/ && CGO_ENABLED=0 go build -ldflags="-w -s" -v -o bin/traffics .
```

### 4. Install to system PATH (optional)
```shell
install -o root -g root -m 0755 bin/traffics /usr/bin/traffics
```

## Usage

Traffics provides two configuration methods: **Command-line parameter configuration** and **Configuration file configuration**.

### Method 1: Command-line Parameter Configuration

Use `-l` (listen) and `-r` (remote) parameters to specify forwarding rules directly, using URL format parameters.

**Note**: You can only specify one `-l` and one `-r` parameter. For multiple forwarding rules, please use the configuration file method.

#### TCP Forwarding
```shell
traffics -l "tcp://:8443?remote=test" -r "1.1.1.1:443?name=test&tfo=true&timeout=10s"

# Traffic path: local:8443 -> 1.1.1.1:443
```

#### UDP Forwarding
```shell
traffics -l "udp://:5353?remote=test" -r "1.1.1.1:53?name=test"

# Traffic path: local:5353 -> 1.1.1.1:53
```

#### TCP + UDP Mixed Forwarding
```shell
traffics -l "tcp+udp://:5353?remote=test" -r "1.1.1.1:53?name=test"

# Traffic path: local:5353 -> 1.1.1.1:53 (supports both TCP and UDP)
```

### Method 2: Configuration File Configuration

Use JSON configuration files for more complex and flexible configurations.

#### Create configuration file
```shell
cat > config.json << EOF
{
  "log": {
    "level": "info"
  },
  "binds": [
    {
      "network": "tcp+udp",
      "remote": "test",
      "listen": "::",
      "port": 5353,
      "name": "test-in"
    }
  ],
  "remotes": [
    {
      "name": "test",
      "server": "1.1.1.1",
      "port": 53
    }
  ]
}
EOF
```

#### Start service
```shell
traffics -c config.json
```

## Configuration Reference

### Log Configuration

Configure logging behavior and verbosity.

```json lines
{
  "disable": false,        // Disable logging (default: false)
  "level": "info"         // Log level: trace, debug, info, warn, error, fatal, panic
}
```

### Bind Configuration (Listen Configuration)

Configure local listening ports and related parameters.

```json lines
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

### Remote Configuration (Target Configuration)

Configure connection parameters for forwarding target servers.

```json lines
{
  // Required fields
  "server": "1.1.1.1",    // Target server address
  "port": 53,             // Target server port
  "name": "remote_name",  // Remote service name (corresponds to remote field in bind)
  
  // Optional fields
  "dns": "8.8.8.8",           // Custom DNS server
  "resolve_strategy": "prefer_ipv4", // DNS resolution strategy: prefer_ipv4/prefer_ipv6/ipv4_only/ipv6_only
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

## Complete Configuration Examples

### Multi-service Forwarding Configuration
```json
{
  "log": {
    "disable": false,
    "level": "info"
  },
  "binds": [
    {
      "network": "tcp+udp",
      "remote": "dns_server",
      "listen": "::",
      "port": 5353,
      "name": "dns_proxy",
      "udp_ttl": "60s"
    }
  ],
  "remotes": [
    {
      "name": "dns_server",
      "server": "1.1.1.1",
      "port": 53,
      "timeout": "10s",
      "resolve_strategy": "prefer_ipv4",
      "tfo": true
    }
  ]
}
```

### Single Service with URL Parameters
```shell
# Single TCP forwarding with advanced options
traffics -l "tcp://0.0.0.0:8080?remote=web_server" -r "192.168.1.100:80?name=web_server&timeout=10s&tfo=true"

# Single UDP DNS forwarding
traffics -l "udp://[::]:5353?remote=dns_server&udp_ttl=30s" -r "1.1.1.1:53?name=dns_server&resolve_strategy=prefer_ipv4"

# Mixed protocol game server forwarding
traffics -l "tcp+udp://0.0.0.0:25565?remote=game_server" -r "game.example.com:25565?name=game_server&timeout=15s&tfo=true"
```

## Use Cases

- **Port Forwarding**: Forward local port traffic to remote servers
- **Load Balancer Frontend**: Act as a simple traffic distributor
- **Network Tunneling**: Bypass firewall restrictions
- **Protocol Proxy**: TCP/UDP protocol conversion and proxying
- **Development Debugging**: Service proxy for local development environments

## Important Notes

1. When using `tcp+udp` mode, both TCP and UDP traffic will be forwarded to the same remote service
2. The `remote` field must match the `name` field in the `remotes` configuration
3. Command-line parameters (`-l` and `-r`) only support single forwarding rules. For multiple rules, use configuration files
4. It's recommended to use configuration files in production environments for easier management and maintenance
5. UDP forwarding supports session persistence, controlled by the `udp_ttl` timeout setting
6. Log levels available: `debug`, `info`, `warn`, `error` (default: `info`)
7. Setting `log.disable` to `true` will completely disable logging output

## Acknowledgments

This project uses the following excellent open-source libraries:

- **[tfo-go](https://github.com/metacubex/tfo-go)** - TCP Fast Open implementation for Go
- **[dns](https://github.com/miekg/dns)** - DNS library in Go
- **[sing](https://github.com/sagernet/sing)** - Universal proxy platform

We are grateful to all contributors and maintainers of these projects for their valuable work.