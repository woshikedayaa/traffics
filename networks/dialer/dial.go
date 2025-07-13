package dialer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/metacubex/tfo-go"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/metadata"
	"github.com/woshikedayaa/traffics/networks/constant"
	"github.com/woshikedayaa/traffics/networks/resolver"
	"net"
	"net/netip"
	"runtime"
	"strconv"
	"time"
)

var tfoInitData = []byte{0}

type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	ListenPacket(ctx context.Context, source netip.Addr, address string) (*net.UDPConn, error)
}

type DialConfig struct {
	Resolver        resolver.Resolver
	ResolveStrategy resolver.Strategy
	Timeout         time.Duration
	Interface       string
	BindAddress4    netip.Addr
	BindAddress6    netip.Addr
	FwMark          uint32
	ReuseAddr       bool
	// tcp
	TFO   bool
	MPTCP bool

	// udp
	UDPFragment bool
}

func NewDefault(config DialConfig) (*DefaultDialer, error) {
	var (
		dialer   net.Dialer
		listener net.ListenConfig
	)
	if config.Interface != "" {
		finder := control.NewDefaultInterfaceFinder()
		bindFunc := control.BindToInterface(finder, config.Interface, -1)
		dialer.Control = control.Append(dialer.Control, bindFunc)
		listener.Control = control.Append(listener.Control, bindFunc)
	}
	if config.Resolver == nil {
		config.Resolver = resolver.NewSystemResolver()
	}
	if config.FwMark != 0 {
		if runtime.GOOS != "linux" {
			return nil, errors.New("`routing_mark` is only supported on Linux")
		}
		dialer.Control = control.Append(dialer.Control, control.RoutingMark(config.FwMark))
		listener.Control = control.Append(listener.Control, control.RoutingMark(config.FwMark))
	}
	dialer.Timeout = cmp.Or(config.Timeout, constant.DialerDefaultTimeout)

	// TODO: customize keepAlive(dialer)
	dialer.KeepAliveConfig = net.KeepAliveConfig{
		Enable:   true,
		Idle:     constant.KeepAliveInitial,
		Interval: constant.KeepAliveInterval,
		Count:    constant.KeepAliveProbeCount,
	}
	if config.ReuseAddr {
		listener.Control = control.Append(listener.Control, control.ReuseAddr())
	}

	if !config.UDPFragment {
		dialer.Control = control.Append(dialer.Control, control.DisableUDPFragment())
		listener.Control = control.Append(listener.Control, control.DisableUDPFragment())
	}
	if config.MPTCP {
		dialer.SetMultipathTCP(true)
	}

	var (
		dialer4 net.Dialer
		dialer6 net.Dialer

		udpDialer4 net.Dialer
		udpDialer6 net.Dialer

		udpAddr4 string
		udpAddr6 string
	)

	if config.BindAddress4.IsValid() {
		bind := config.BindAddress4
		dialer4.LocalAddr = &net.TCPAddr{IP: bind.AsSlice()}
		udpDialer4.LocalAddr = &net.UDPAddr{IP: bind.AsSlice()}
		udpAddr4 = bind.String()
	}

	if config.BindAddress6.IsValid() {
		bind := config.BindAddress6
		dialer6.LocalAddr = &net.TCPAddr{IP: bind.AsSlice()}
		udpDialer6.LocalAddr = &net.UDPAddr{IP: bind.AsSlice()}
		udpAddr6 = bind.String()
	}

	return &DefaultDialer{
		defaultDialer: dialer,
		dialer4: tfo.Dialer{
			Dialer:     dialer4,
			DisableTFO: !config.TFO,
		},
		dialer6: tfo.Dialer{
			Dialer:     dialer6,
			DisableTFO: !config.TFO,
		},
		udpDialer4:      udpDialer4,
		udpDialer6:      udpDialer6,
		udpAddr4:        udpAddr4,
		udpAddr6:        udpAddr6,
		resolver:        config.Resolver,
		resolveStrategy: config.ResolveStrategy,
	}, nil
}

type DefaultDialer struct {
	defaultDialer net.Dialer

	dialer4 tfo.Dialer
	dialer6 tfo.Dialer

	udpDialer4 net.Dialer
	udpDialer6 net.Dialer

	udpAddr4 string
	udpAddr6 string

	resolver        resolver.Resolver
	resolveStrategy resolver.Strategy
}

func (d *DefaultDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	switch network {
	case "udp", "udp4", "udp6", "tcp", "tcp4", "tcp6":
	default:
		// fallback to default dialer
		return d.defaultDialer.DialContext(ctx, network, address)
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("dialer: split host port failed: %s: %w", address, err)
	}
	portNum, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("dialer: invalid port number: %s: %w", port, err)
	}
	if !metadata.IsDomainName(host) {
		addr, err := netip.ParseAddr(host)
		if err != nil {
			return nil, fmt.Errorf("dialer: invalid address: %s: %w", host, err)
		}
		return d.DialSerial(ctx, network, []netip.Addr{addr}, uint16(portNum))
	}
	a, aaaa, err := d.resolver.Lookup(ctx, host, d.resolveStrategy)
	if err != nil {
		return nil, fmt.Errorf("dialer: resolve address failed: %w", err)
	}

	return d.DialParallel(ctx, network, a, aaaa, uint16(portNum))
}

func (d *DefaultDialer) DialSerial(ctx context.Context, network string, addresses []netip.Addr, port uint16) (net.Conn, error) {
	if len(addresses) == 0 {
		return nil, errors.New("dialer: no addresses to dial")
	}
	nn, networkErr := constant.ParseNetwork(network)
	if networkErr != nil {
		return nil, networkErr
	}

	availableAddress := filterAddressByNetwork(nn, addresses)
	if len(availableAddress) == 0 {
		return nil, fmt.Errorf("dialer: no available address found for network: %s", network)
	}

	var lastErr error
	for _, addr := range availableAddress {
		if common.Done(ctx) {
			return nil, ctx.Err()
		}
		var (
			target    = netip.AddrPortFrom(addr, port)
			conn      net.Conn
			err       error
			tcpDialer *tfo.Dialer
			udpDialer *net.Dialer
		)
		switch {
		case addr.Is4():
			udpDialer = &d.udpDialer4
			tcpDialer = &d.dialer4
		case addr.Is6():
			udpDialer = &d.udpDialer4
			tcpDialer = &d.dialer4
		default:
			tcpDialer = &tfo.Dialer{Dialer: d.defaultDialer, DisableTFO: true, Fallback: false}
			udpDialer = &d.defaultDialer
		}
		switch nn.Protocol {
		case constant.ProtocolUDP:
			conn, err = udpDialer.DialContext(ctx, network, target.String())
		case constant.ProtocolTCP:
			if tcpDialer.DisableTFO {
				conn, err = tcpDialer.DialContext(ctx, network, target.String(), nil)
			} else {
				conn, err = tcpDialer.DialContext(ctx, network, target.String(), tfoInitData)
			}
		default:
			conn, err = d.defaultDialer.DialContext(ctx, network, addr.String())
		}

		if err == nil {
			return conn, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("dialer: all addresses failed, last error: %w", lastErr)
}

func (d *DefaultDialer) DialParallel(ctx context.Context, network string,
	ipv4 []netip.Addr, ipv6 []netip.Addr, port uint16) (net.Conn, error) {
	if len(ipv4) == 0 {
		return d.DialSerial(ctx, network, ipv6, port)
	}
	if len(ipv6) == 0 {
		return d.DialSerial(ctx, network, ipv4, port)
	}

	// happy eyeball implement
	type dialResult struct {
		conn net.Conn
		err  error
		ipv6 bool
	}

	resultChan := make(chan dialResult, 2)
	dialCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// as RFC6555 said: prefer ipv6
	go func() {
		conn, err := d.DialSerial(dialCtx, network, ipv6, port)
		select {
		case resultChan <- dialResult{conn: conn, err: err, ipv6: true}:
		case <-dialCtx.Done():
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// happy eyeball
	ipv4Timer := time.NewTimer(300 * time.Millisecond)
	defer ipv4Timer.Stop()

	var ipv4Started bool
	var resultsReceived int

	for resultsReceived < 2 {
		select {
		case <-dialCtx.Done():
			return nil, dialCtx.Err()

		case <-ipv4Timer.C:
			if !ipv4Started {
				ipv4Started = true
				go func() {
					conn, err := d.DialSerial(dialCtx, network, ipv4, port)
					select {
					case resultChan <- dialResult{conn: conn, err: err, ipv6: false}:
					case <-dialCtx.Done():
						if conn != nil {
							conn.Close()
						}
					}
				}()
			}

		case result := <-resultChan:
			resultsReceived++

			if result.err == nil {
				cancel()
				return result.conn, nil
			}

			if !ipv4Started && resultsReceived == 1 {
				ipv4Started = true
				ipv4Timer.Stop()
				go func() {
					conn, err := d.DialSerial(dialCtx, network, ipv4, port)
					select {
					case resultChan <- dialResult{conn: conn, err: err, ipv6: false}:
					case <-dialCtx.Done():
						if conn != nil {
							conn.Close()
						}
					}
				}()
			}
		}
	}

	return nil, fmt.Errorf("dialer: all parallel dials failed for both IPv4 and IPv6")
}

func (d *DefaultDialer) ListenPacket(ctx context.Context, source netip.Addr, address string) (*net.UDPConn, error) {
	return nil, nil
}

func filterAddressByNetwork(network constant.Network, addr []netip.Addr) []netip.Addr {
	switch {
	case network.Version == constant.NetworkVersionDual:
		return common.Filter(addr, func(it netip.Addr) bool {
			return it.IsValid()
		})
	case network.Version == constant.NetworkVersion4:
		return common.Filter(addr, func(it netip.Addr) bool {
			return it.IsValid() && it.Is4()
		})
	case network.Version == constant.NetworkVersion6:
		return common.Filter(addr, func(it netip.Addr) bool {
			return it.IsValid() && it.Is6()
		})
	default:
		return addr
	}
}
