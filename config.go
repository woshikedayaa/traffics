package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/woshikedayaa/traffics/networks/constant"
	"github.com/woshikedayaa/traffics/networks/resolver"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Binds  []BindConfig   `json:"binds,omitempty"`
	Remote []RemoteConfig `json:"remotes,omitempty"`
	Log    LogConfig      `json:"log,omitempty"`
}

func NewConfig() Config {
	return Config{
		Binds:  []BindConfig{},
		Remote: []RemoteConfig{},
		Log:    LogConfig{},
	}
}

type LogConfig struct {
	Disable bool   `json:"disable,omitempty"`
	Level   string `json:"level,omitempty"`
}

type BindConfig struct {
	Raw string `json:"-,omitempty"`

	// metadata(required)
	Network constant.Protocol `json:"network,omitempty"`
	Listen  netip.Addr        `json:"listen,omitempty"`
	Port    uint16            `json:"port,omitempty"`
	Remote  string            `json:"remote,omitempty"`
	// metadata(optional)
	Name string `json:"name,omitempty"`

	// below is configured by args
	Family    string `json:"family,omitempty"`
	Interface string `json:"interface,omitempty"`
	// TProxy    bool   `json:"tproxy,omitempty"`
	ReuseAddr bool `json:"reuse_addr,omitempty"`

	// tcp
	TFO bool `json:"tfo,omitempty"`
	// Redirect bool `json:"redirect,omitempty"`
	MPTCP bool `json:"mptcp,omitempty"`

	// udp configuration
	UDPKeepaliveTTL time.Duration `json:"udp_ttl,omitempty"`
	UDPBufferSize   int           `json:"udp_buffer_size,omitempty"` // byte
	UDPFragment     bool          `json:"udp_fragment,omitempty"`
}

type _BindConfig BindConfig

func NewDefaultBind() BindConfig {
	return BindConfig{
		UDPKeepaliveTTL: 60 * time.Second,
		UDPBufferSize:   65507,
	}
}

func (c *BindConfig) valid() error {
	if c.Network == "" {
		c.Network = constant.ProtocolTCPUDP
	}
	if c.Listen.IsValid() {
		if c.Listen.Is6() && c.Family == constant.FamilyIPv4 {
			return fmt.Errorf("listen: listen a ipv6 address with ipv4 family")
		}
		if c.Listen.Is4() && c.Family == constant.FamilyIPv6 {
			return fmt.Errorf("listen: listen a ipv4 address with ipv6 family")
		}
	} else {
		c.Listen = netip.IPv6Unspecified()
	}

	if c.Port == 0 {
		return errors.New("listen: no port specified")
	}
	if c.Remote == "" {
		return errors.New("listen: no remote")
	}
	return nil
}

func (c *BindConfig) Parse(s string) error {
	if s == "" {
		return errors.New("parse bind: empty string")
	}

	uu, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("parse bind: %w", err)
	}

	c.Raw = s
	var listenAddress netip.Addr
	if uu.Hostname() == "" {
		listenAddress = netip.IPv6Unspecified()
	} else {
		listenAddress, err = netip.ParseAddr(uu.Hostname())
		if err != nil {
			return fmt.Errorf("prase bind(listen): %w", err)
		}
	}
	c.Listen = listenAddress
	if uu.Port() != "" {
		pp, err := strconv.ParseUint(uu.Port(), 10, 16)
		if err != nil {
			return fmt.Errorf("parse bind(port): %w", err)
		}
		c.Port = uint16(pp)
	}

	if uu.Scheme != "" {
		c.Network = constant.ParseProtocol(uu.Scheme)
	}

	for k, v := range uu.Query() {
		if len(v) == 0 {
			continue
		}

		switch k {
		case "family":
			c.Family = v[0]
		case "interface":
			c.Interface = v[0]
		case "reuse_addr":
			ok, err := strconv.ParseBool(v[0])
			if err != nil {
				return fmt.Errorf("parse bind(reuse_addr): expected bool, got %s", v[0])
			}
			c.ReuseAddr = ok
		case "name":
			c.Name = v[0]
		case "tfo":
			ok, err := strconv.ParseBool(v[0])
			if err != nil {
				return fmt.Errorf("parse bind(tfo): expected bool, got %s", v[0])
			}
			c.TFO = ok
		case "udp_ttl":
			duration, err := time.ParseDuration(v[0])
			if err != nil {
				return fmt.Errorf("parse bind(udp_ttl): %w", err)
			}
			c.UDPKeepaliveTTL = duration
		case "remote":
			c.Remote = v[0]
		case "udp_buffer_size":
			size, err := strconv.Atoi(v[0])
			if err != nil {
				return fmt.Errorf("parse bind(udp_buffer_size): %w", err)
			}
			c.UDPBufferSize = size
		case "udp_fragment":
			ok, err := strconv.ParseBool(v[0])
			if err != nil {
				return fmt.Errorf("parse bind(udp_fragment): expected bool, got %s", v[0])
			}
			c.UDPFragment = ok
		case "mptcp":
			ok, err := strconv.ParseBool(v[0])
			if err != nil {
				return fmt.Errorf("parse bind(mptcp): expected bool, got %s", v[0])
			}
			c.MPTCP = ok
		default:
			return fmt.Errorf("parse bind: unknown option: %s", k)
		}
	}

	return c.valid()
}

func (c *BindConfig) UnmarshalJSON(bs []byte) error {
	rawStr := string(bs)
	if len(rawStr) >= 2 && rawStr[0] == '"' && rawStr[len(rawStr)-1] == '"' {
		rawStr = rawStr[1 : len(rawStr)-1]
	}

	_, err := url.Parse(rawStr)
	if err == nil && strings.Contains(rawStr, "://") {
		return c.Parse(rawStr)
	}
	err = json.Unmarshal(bs, (*_BindConfig)(c))
	if err != nil {
		return err
	}
	return c.valid()
}

type RemoteConfig struct {
	Raw string `json:"-,omitempty"`

	// metadata(required)
	Name   string `json:"name,omitempty"`
	Server string `json:"server,omitempty"`
	Port   uint16 `json:"port,omitempty"`

	// optional
	DNS             string            `json:"dns,omitempty"`
	ResolveStrategy resolver.Strategy `json:"resolve_strategy,omitempty"`
	Timeout         time.Duration     `json:"timeout,omitempty"`
	ReuseAddr       bool              `json:"reuse_addr,omitempty"`
	Interface       string            `json:"interface,omitempty"`
	BindAddress4    netip.Addr        `json:"bind_address_4,omitempty"`
	BindAddress6    netip.Addr        `json:"bind_address_6,omitempty"`
	FwMark          uint32            `json:"fw_mark,omitempty"`

	// tcp
	TFO   bool `json:"tfo,omitempty"`
	MPTCP bool `json:"mptcp,omitempty"`

	// udp
	UDPFragment bool `json:"udp_fragment,omitempty"`
}

type _RemoteConfig RemoteConfig

func NewDefaultRemote() RemoteConfig {
	return RemoteConfig{
		Timeout: 10 * time.Second, // default timeout
	}
}

func (c *RemoteConfig) valid() error {
	if c.Name == "" {
		return errors.New("dialer: no name specified")
	}
	if c.Server == "" {
		return errors.New("dialer: no server specified")
	}
	if c.Port == 0 {
		return errors.New("dialer: no server port specified")
	}

	return nil
}

func (c *RemoteConfig) Parse(s string) error {
	if s == "" {
		return errors.New("parse remote: empty string")
	}

	uu, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("parse remote: %w", err)
	}

	c.Raw = s
	c.Server = uu.Hostname()

	if uu.Port() != "" {
		pp, err := strconv.ParseUint(uu.Port(), 10, 16)
		if err != nil {
			return fmt.Errorf("parse remote(port): %w", err)
		}
		c.Port = uint16(pp)
	}

	for k, v := range uu.Query() {
		if len(v) == 0 {
			continue
		}

		switch k {
		case "dns":
			c.DNS = v[0]
		case "resolve_strategy":
			strategy, ok := resolver.ParseStrategy(v[0])
			if !ok {
				return errors.New(fmt.Sprintf("resolve: unsupported resolve policy: %s", v[0]))
			}
			c.ResolveStrategy = strategy
		case "timeout":
			timeout, err := time.ParseDuration(v[0])
			if err != nil {
				return fmt.Errorf("parse remote(timeout):  expected duration, got %s", v[0])
			}
			c.Timeout = timeout
		case "reuse_addr":
			ok, err := strconv.ParseBool(v[0])
			if err != nil {
				return fmt.Errorf("parse remote(reuse_addr):  expected bool, got %s", v[0])
			}
			c.ReuseAddr = ok
		case "tfo":
			ok, err := strconv.ParseBool(v[0])
			if err != nil {
				return fmt.Errorf("parse remote(tfo): expected bool, got %s", v[0])
			}
			c.TFO = ok
		case "fw_mark":
			mark, err := strconv.ParseUint(v[0], 10, 32)
			if err != nil {
				return fmt.Errorf("parse remote(fw_mark): %w", err)
			}
			c.FwMark = uint32(mark)
		case "udp_fragment":
			ok, err := strconv.ParseBool(v[0])
			if err != nil {
				return fmt.Errorf("parse remote(udp_fragment):  expected bool, got %s", v[0])
			}
			c.UDPFragment = ok
		case "interface":
			c.Interface = v[0]
		case "mptcp":
			mptcp, err := strconv.ParseBool(v[0])
			if err != nil {
				return fmt.Errorf("parse remote(mptcp):  expected bool, got %s: %w", v[0], err)
			}
			c.MPTCP = mptcp
		case "bind_address_4":
			addr, err := netip.ParseAddr(v[0])
			if err != nil {
				return fmt.Errorf("parse remote(bind_address_4): %w", err)
			}
			c.BindAddress4 = addr
		case "bind_address_6":
			addr, err := netip.ParseAddr(v[0])
			if err != nil {
				return fmt.Errorf("parse remote(bind_address_6): %w", err)
			}
			c.BindAddress6 = addr
		case "name":
			c.Name = v[0]
		default:
			return fmt.Errorf("parse remote: unknown option: %s", k)
		}
	}

	return c.valid()
}

func (c *RemoteConfig) UnmarshalJSON(bs []byte) error {
	rawStr := string(bs)
	if len(rawStr) >= 2 && rawStr[0] == '"' && rawStr[len(rawStr)-1] == '"' {
		rawStr = rawStr[1 : len(rawStr)-1]
	}

	_, err := url.Parse(rawStr)
	if err == nil && strings.Contains(rawStr, "://") {
		return c.Parse(rawStr)
	}
	err = json.Unmarshal(bs, (*_RemoteConfig)(c))
	if err != nil {
		return err
	}
	return c.valid()
}
