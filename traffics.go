package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/sagernet/sing/common/bufio"
	"github.com/woshikedayaa/traffics/networks/constant"
	"github.com/woshikedayaa/traffics/networks/dialer"
	"github.com/woshikedayaa/traffics/networks/listener"
	"github.com/woshikedayaa/traffics/networks/resolver"
	"log/slog"
	"math/rand"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type Traffics struct {
	ctx    context.Context
	cancel context.CancelFunc

	config Config

	logger *slog.Logger

	listeners *ListenManager

	nameToDialer map[string]struct {
		address string
		dialer  dialer.Dialer
	}

	// udpConnTrack *cache.LruCache[netip.AddrPort, *net.UDPConn]
	udpConnTrack *sync.Map
}

func NewTraffics(ctx context.Context, config Config) (*Traffics, error) {
	t := &Traffics{}
	rootCtx, cancel := context.WithCancel(ctx)
	t.ctx = rootCtx
	t.cancel = cancel
	t.config = config
	t.nameToDialer = make(map[string]struct {
		address string
		dialer  dialer.Dialer
	})
	t.listeners = NewListenManager()
	t.udpConnTrack = &sync.Map{}

	var err error
	t.logger, err = newLogger(config.Log)
	if err != nil {
		return nil, err
	}

	if len(config.Remote) == 1 && len(config.Binds) == 1 && config.Binds[0].Remote == "" {
		config.Binds[0].Remote = config.Remote[0].Name
	}

	return t, nil
}

func (t *Traffics) Close() error {
	t.cancel()
	t.listeners.CloseAll()
	t.udpConnTrack.Range(func(key, value any) bool {
		if conn, ok := value.(*net.UDPConn); ok {
			conn.Close()
		}
		return true
	})
	return nil
}

func (t *Traffics) Start() error {
	// init dialer first
	return errors.Join(
		t.initDialer(),
		t.initListener(),
		t.listeners.StartAll(),
	)
}

func (t *Traffics) initDialer() error {
	var systemResolver resolver.Resolver = resolver.NewSystemResolver()
	// build dialer first
	for _, v := range t.config.Remote {
		if v.Name == "" {
			// TODO: provide more detailed info about this
			return fmt.Errorf("no name specified for %s", v.Server)
		}

		if _, ok := t.nameToDialer[v.Name]; ok {
			return fmt.Errorf("duplicated remote name: %s", v.Name)
		}
		realResolvePolicy := v.ResolveStrategy
		realResolver := systemResolver
		if v.DNS != "" {
			realResolver = resolver.NewCachedResolverDefault(
				resolver.NewRawClient(net.Dialer{}, v.DNS))
		}
		var bind4, bind6 netip.Addr
		bind4 = v.BindAddress4
		bind6 = v.BindAddress6

		dd, err := dialer.NewDefault(dialer.DialConfig{
			Resolver:        realResolver,
			Timeout:         cmp.Or(v.Timeout, constant.DialerDefaultTimeout),
			Interface:       v.Interface,
			BindAddress4:    bind4,
			BindAddress6:    bind6,
			FwMark:          v.FwMark,
			ReuseAddr:       v.ReuseAddr,
			TFO:             v.TFO,
			MPTCP:           v.MPTCP,
			UDPFragment:     v.UDPFragment,
			ResolveStrategy: realResolvePolicy,
		})
		if err != nil {
			return err
		}
		t.nameToDialer[v.Name] = struct {
			address string
			dialer  dialer.Dialer
		}{address: net.JoinHostPort(v.Server, strconv.FormatUint(uint64(v.Port), 10)), dialer: dd}
	}
	return nil
}

func (t *Traffics) initListener() error {
	// parse listener
	for _, v := range t.config.Binds {

		var name = v.Name
		if v.Name == "" {
			name = netip.AddrPortFrom(v.Listen, v.Port).String()
		}

		if v.Remote == "" {
			return fmt.Errorf("no remote specified for %s", name)
		}

		logger := t.logger.With(slog.String("listener", name))
		protocols := v.Network.ToProtocolList()

		dial, ok := t.nameToDialer[v.Remote]
		if !ok {
			return fmt.Errorf("no remote with name: %s", v.Remote)
		}

		li := listener.NewListener(t.ctx, logger, listener.ListenOptions{
			Network:       protocols,
			Address:       v.Listen,
			Port:          v.Port,
			Family:        v.Family,
			Interface:     v.Interface,
			ReuseAddr:     v.ReuseAddr,
			TFO:           v.TFO,
			MPTCP:         v.MPTCP,
			UDPFragment:   v.UDPFragment,
			UDPBufferSize: v.UDPBufferSize,
			PacketHandler: (*TrafficHandler)(t).PacketHandler(
				protocols.Contain(string(constant.ProtocolUDP)),
				logger,
				v,
				dial.dialer,
				dial.address,
			),
			ConnHandler: (*TrafficHandler)(t).ConnHandler(
				protocols.Contain(string(constant.ProtocolTCP)),
				logger,
				dial.dialer,
				dial.address,
			),
		})
		t.listeners.Add(li)
	}
	return nil
}

func newLogger(config LogConfig) (*slog.Logger, error) {
	if config.Disable {
		return slog.New(slog.DiscardHandler), nil
	}

	var logger *slog.Logger
	level := slog.Level(0)
	if config.Level != "" {
		err := level.UnmarshalText([]byte(config.Level))
		if err != nil {
			return nil, err
		}
	}

	logger = slog.New(slog.NewTextHandler(
		os.Stdout, &slog.HandlerOptions{
			Level: level,
		}))

	return logger, nil
}

type TrafficHandler Traffics

func (t *TrafficHandler) PacketHandler(
	enable bool, logger *slog.Logger, config BindConfig,
	dial dialer.Dialer, address string,
) listener.PacketHandler {
	if !enable {
		return nil
	}

	return listener.FuncPacketHandler(func(p []byte, remote netip.AddrPort, pw listener.PacketWriter) {
		if !remote.IsValid() {
			logger.ErrorContext(t.ctx, "invalid address")
		}

		if raw, hit := t.udpConnTrack.Load(remote); hit {
			conn := raw.(*net.UDPConn)
			_, err := conn.Write(p)
			if err != nil {
				logger.ErrorContext(t.ctx, "write message error", slog.String("error", err.Error()))
			}
			return
		}

		logger.DebugContext(t.ctx, "try dial new connection", slog.String("address", address))
		conn, err := dial.DialContext(t.ctx, string(constant.ProtocolUDP), address)
		if err != nil {
			logger.ErrorContext(t.ctx, "dial udp conn failed",
				slog.String("error", err.Error()), slog.String("remote", address))
			return
		}
		var id = rand.Int63()
		logger = logger.With(slog.Int64("id", id))
		if udpConn, ok := conn.(*net.UDPConn); ok {
			t.udpConnTrack.Store(remote, udpConn)
			go t.newUdpLoop(logger, remote, udpConn, pw, config)
			logger.DebugContext(t.ctx, "new udp connection established",
				slog.String("source", remote.String()),
				slog.String("remote", udpConn.RemoteAddr().String()))

			_, err = udpConn.Write(p)
			if err != nil {
				logger.ErrorContext(t.ctx, "write udp message failed", slog.String("error", err.Error()))
			}
		} else {
			panic("DialContext in udp network returned a non-udpConn")
		}
	})
}

func (t *TrafficHandler) newUdpLoop(logger *slog.Logger, client netip.AddrPort, proxyConn *net.UDPConn,
	pw listener.PacketWriter, config BindConfig) {
	defer func() {
		t.udpConnTrack.Delete(client)
		proxyConn.Close()
		logger.DebugContext(t.ctx, "udp connection closed")
	}()

	readBuf := make([]byte, config.UDPBufferSize)
	for {
		proxyConn.SetReadDeadline(time.Now().Add(config.UDPKeepaliveTTL))
	again:
		read, err := proxyConn.Read(readBuf)
		if err != nil {
			var ope *net.OpError
			if errors.As(err, &ope) && errors.Is(ope.Err, syscall.ECONNREFUSED) {
				// This will happen if the last write failed
				// (e.g: nothing is actually listening on the
				// proxied port on the container), ignore it
				// and continue until UDPConnTrackTimeout
				// expires:
				goto again
			}
			return
		}
		if read != 0 {
			pw.WritePacket(readBuf[:read], client)
		}
	}
}

func (t *TrafficHandler) ConnHandler(
	enable bool, logger *slog.Logger,
	dial dialer.Dialer, address string,
) listener.ConnHandler {
	if !enable {
		return nil
	}

	return listener.FuncConnHandler(func(ctx context.Context, local net.Conn) {
		defer local.Close()
		var (
			remote net.Conn
			err    error
			id     = rand.Int63()
		)
		if remote, err = dial.DialContext(t.ctx, string(constant.ProtocolTCP), address); err != nil {
			logger.Error("dial new connection failed", slog.String("error", err.Error()))
			return
		}
		defer remote.Close()

		logger.InfoContext(t.ctx, "new tcp connection established",
			slog.String("source", local.RemoteAddr().String()),
			slog.String("remote", remote.RemoteAddr().String()),
			slog.String("local", remote.LocalAddr().String()),
			slog.Int64("id", id),
		)

		if err = bufio.CopyConn(ctx, local, remote); err != nil {
			logger.Error("copy connections failed", slog.String("error", err.Error()))
			return
		}
		logger.DebugContext(ctx, "copyConn finished", slog.Int64("id", id))
	})
}

type ListenManager struct {
	listeners []*listener.Listener
}

func NewListenManager() *ListenManager {
	return &ListenManager{make([]*listener.Listener, 0)}
}

func (m *ListenManager) Add(li *listener.Listener) {
	m.listeners = append(m.listeners, li)
}
func (m *ListenManager) StartAll() error {
	for _, listen := range m.listeners {
		err := listen.Start()
		if err != nil {
			return err
		}
	}
	return nil
}
func (m *ListenManager) CloseAll() error {
	for _, listen := range m.listeners {
		err := listen.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
