package listener

import (
	"context"
	"fmt"
	"github.com/metacubex/tfo-go"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/control"
	"github.com/woshikedayaa/traffics/networks/constant"
	"log/slog"
	"net"
	"net/netip"
)

type PacketWriter interface {
	WritePacket(bs []byte, remote netip.AddrPort)
}

type PacketHandler interface {
	HandlePacket(p []byte, remote netip.AddrPort, pw PacketWriter)
}

type PacketHandlerOOb interface {
	HandlePacketOOb(oob []byte, p []byte, remote netip.AddrPort, pw PacketWriter)
}

type ConnHandler interface {
	HandleConn(ctx context.Context, conn net.Conn)
}

type (
	FuncPacketHandler func(p []byte, remote netip.AddrPort, pw PacketWriter)
	FuncConnHandler   func(ctx context.Context, conn net.Conn)
)

func (f FuncPacketHandler) HandlePacket(p []byte, remote netip.AddrPort, pw PacketWriter) {
	f(p, remote, pw)
}
func (f FuncConnHandler) HandleConn(ctx context.Context, conn net.Conn) {
	f(ctx, conn)
}

type ListenOptions struct {
	// required
	Network constant.ProtocolList
	Address netip.Addr
	Port    uint16

	// optional
	Family    string
	Interface string
	ReuseAddr bool

	// tcp
	TFO   bool
	MPTCP bool

	// udp
	UDPFragment   bool
	UDPBufferSize int

	// Handler
	PacketHandler    PacketHandler
	PacketHandlerOOb PacketHandlerOOb
	ConnHandler      ConnHandler
}

type Listener struct {
	ctx    context.Context
	logger *slog.Logger

	options          ListenOptions
	packetHandler    PacketHandler
	connHandler      ConnHandler
	packetHandlerOOb PacketHandlerOOb

	// internal
	udpConn     *net.UDPConn
	tcpListener net.Listener
	cancel      context.CancelFunc
}

func NewListener(ctx context.Context, logger *slog.Logger,
	options ListenOptions) *Listener {
	cancelCtx, cancel := context.WithCancel(ctx)

	return &Listener{
		ctx:              cancelCtx,
		logger:           logger,
		options:          options,
		packetHandler:    options.PacketHandler,
		connHandler:      options.ConnHandler,
		packetHandlerOOb: options.PacketHandlerOOb,
		cancel:           cancel,
	}
}

func (l *Listener) Start() error {
	if l.options.Network.Contain(string(constant.ProtocolTCP)) {
		_, err := l.ListenTCP()
		if err != nil {
			return err
		}
		l.logger.InfoContext(l.ctx, "new tcp server started at",
			slog.String("address", l.tcpListener.Addr().String()))
		go l.loopTcp()
	}
	if l.options.Network.Contain(string(constant.ProtocolUDP)) {
		_, err := l.ListenUDP()
		if err != nil {
			return err
		}

		if l.packetHandlerOOb != nil {
			go l.loopUdpInOOb()
		} else {
			go l.loopUdpIn()
		}
		l.logger.InfoContext(l.ctx, "new udp server started at",
			slog.String("address", l.tcpListener.Addr().String()))
		// go l.loopUdpOut()
	}
	return nil
}

func (l *Listener) ListenUDP() (*net.UDPConn, error) {
	var (
		listenConfig net.ListenConfig
	)

	if l.options.Interface != "" {
		var interfaceFinder = control.NewDefaultInterfaceFinder()
		listenConfig.Control = control.Append(listenConfig.Control, control.BindToInterface(interfaceFinder, l.options.Interface, -1))
	}

	if l.options.ReuseAddr {
		listenConfig.Control = control.Append(listenConfig.Control, control.ReuseAddr())
	}
	if !l.options.UDPFragment {
		listenConfig.Control = control.Append(listenConfig.Control, control.DisableUDPFragment())
	}
	var (
		bindAddress = netip.AddrPortFrom(l.options.Address, l.options.Port).String()
		network     string
		err         error
	)
	network = string(constant.ProtocolUDP)
	if l.options.Family == constant.FamilyIPv6 {
		network += constant.FamilyIPv6
	} else if l.options.Family == constant.FamilyIPv4 {
		network += constant.FamilyIPv4
	}

	packetConn, err := listenConfig.ListenPacket(l.ctx, network, bindAddress)

	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	l.udpConn = packetConn.(*net.UDPConn)
	return l.udpConn, nil
}

func (l *Listener) ListenTCP() (net.Listener, error) {
	var (
		listenConfig net.ListenConfig
	)

	if l.options.Interface != "" {
		var interfaceFinder = control.NewDefaultInterfaceFinder()
		listenConfig.Control = control.Append(listenConfig.Control, control.BindToInterface(interfaceFinder, l.options.Interface, -1))
	}

	if l.options.ReuseAddr {
		listenConfig.Control = control.Append(listenConfig.Control, control.ReuseAddr())
	}
	// TODO: customize keepAlive(listen)
	listenConfig.KeepAliveConfig = net.KeepAliveConfig{
		Enable:   true,
		Idle:     constant.KeepAliveInitial,
		Interval: constant.KeepAliveInterval,
		Count:    constant.KeepAliveProbeCount,
	}
	if l.options.MPTCP {
		listenConfig.SetMultipathTCP(true)
	}
	var (
		bindAddress = netip.AddrPortFrom(l.options.Address, l.options.Port).String()
		listener    net.Listener
		network     string
		err         error
	)
	network = string(constant.ProtocolTCP)
	if l.options.Family == constant.FamilyIPv6 {
		network += constant.FamilyIPv6
	} else if l.options.Family == constant.FamilyIPv4 {
		network += constant.FamilyIPv4
	}

	if l.options.TFO {
		var tfoConfig tfo.ListenConfig
		tfoConfig.ListenConfig = listenConfig
		listener, err = tfoConfig.Listen(l.ctx, network, bindAddress)
	} else {
		listener, err = listenConfig.Listen(l.ctx, network, bindAddress)
	}

	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	l.tcpListener = listener
	return listener, nil
}

func (l *Listener) Close() error {
	l.cancel()
	if l.tcpListener != nil {
		l.tcpListener.Close()
	}
	if l.udpConn != nil {
		l.udpConn.Close()
	}
	return nil
}

func (l *Listener) loopUdpIn() {
	buf := make([]byte, l.options.UDPBufferSize)
	for l.udpConn != nil {
		n, remote, err := l.udpConn.ReadFromUDPAddrPort(buf[0:l.options.UDPBufferSize])
		if err != nil {
			if common.Done(l.ctx) {
				return
			}
			l.logger.Error("read udp message", slog.String("error", err.Error()))
			continue
		}
		//if n == 0 {
		//	l.logger.Warn("read a zero size udp message without error")
		//	continue
		//}
		l.packetHandler.HandlePacket(buf[:n], remote, l)
	}
}

func (l *Listener) loopUdpInOOb() {
	buf := make([]byte, l.options.UDPBufferSize)
	oob := make([]byte, 4096)
	for l.udpConn != nil {
		n, oobN, _, remote, err := l.udpConn.ReadMsgUDPAddrPort(buf[0:l.options.UDPBufferSize], oob[0:len(oob)])
		if err != nil {
			if common.Done(l.ctx) {
				return
			}
			l.logger.Error("read udp message", slog.String("error", err.Error()))
			continue
		}
		if n == 0 {
			l.logger.Warn("read a zero size udp message without error")
			continue
		}
		l.packetHandlerOOb.HandlePacketOOb(oob[:oobN], buf[:n], remote, l)
	}
}

//func (l *Listener) loopUdpOut() {
//	var isClose = false
//	for !isClose {
//		select {
//		case pp := <-l.packetWriter:
//			if !pp.Remote.IsValid() {
//				l.logger.WarnContext(l.ctx, "invalid address for packet writer")
//				continue
//			}
//
//			nn, err := l.udpConn.WriteToUDPAddrPort(pp.Data, pp.Remote)
//			_ = nn
//			if err != nil {
//				l.logger.ErrorContext(l.ctx, "write udp message", err)
//			}
//		case <-l.ctx.Done():
//			for pp := range l.packetWriter {
//				_ = pp // discard
//			}
//			isClose = true
//		}
//	}
//}

func (l *Listener) WritePacket(bs []byte, remote netip.AddrPort) {
	if common.Done(l.ctx) {
		return
	}
	nn, err := l.udpConn.WriteToUDPAddrPort(bs, remote)
	_ = nn
	if err != nil {
		l.logger.ErrorContext(l.ctx, "write udp message", err)
	}
}

func (l *Listener) loopTcp() {
	for l.tcpListener != nil {
		conn, err := l.tcpListener.Accept()
		if err != nil {
			if common.Done(l.ctx) {
				return
			}
			l.logger.ErrorContext(l.ctx, "accept",
				slog.String("error", err.Error()))
			continue
		}
		go l.connHandler.HandleConn(l.ctx, conn)
	}
}
