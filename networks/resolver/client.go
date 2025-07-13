package resolver

import (
	"context"
	"errors"
	"fmt"
	"github.com/miekg/dns"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/task"
	"github.com/woshikedayaa/traffics/networks/constant"
	"io"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"time"
)

const (
	maxConn = 8
)

type RawClient struct {
	dialer      net.Dialer
	destination string

	conns chan net.Conn // max = maxConn

	connCount atomic.Int32
}

func NewRawClient(dialer net.Dialer, destination string) *RawClient {
	return &RawClient{
		dialer:      dialer,
		destination: destination,
		conns:       make(chan net.Conn, maxConn),
	}
}

func (c *RawClient) Lookup(ctx context.Context, fqdn string, strategy Strategy) (A []netip.Addr, AAAA []netip.Addr, err error) {
	group := task.Group{}

	if strategy != StrategyIPv6Only {
		group.Append0(func(ctx context.Context) error {
			resp, internal := c.lookupToExchange(ctx, fqdn, dns.TypeA)
			if internal != nil || resp == nil {
				return internal
			}
			A = append(A, resp...)
			return nil
		})
	}
	if strategy != StrategyIPv4Only {
		group.Append0(func(ctx context.Context) error {
			resp, internal := c.lookupToExchange(ctx, fqdn, dns.TypeAAAA)
			if internal != nil || resp == nil {
				return internal
			}
			AAAA = append(AAAA, resp...)
			return nil
		})
	}
	err = group.Run(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve: %w", err)
	}
	A, AAAA = FilterAddress(A, AAAA, strategy)
	return A, AAAA, nil
}

func (c *RawClient) lookupToExchange(ctx context.Context, fqdn string, queryType uint16) (address []netip.Addr, err error) {
	question := &dns.Msg{
		MsgHdr: dns.MsgHdr{
			Id:               dns.Id(),
			RecursionDesired: true,
		},
		Question: []dns.Question{
			{Name: fqdn, Qtype: queryType, Qclass: dns.ClassINET},
		},
	}
	resp, err := c.exchange(
		ctx,
		question,
	)
	if err != nil {
		return nil, err
	}

	return MessageToAddresses(resp)
}

func (c *RawClient) Exchange(ctx context.Context, request *dns.Msg) (answer *dns.Msg, err error) {
	return c.exchange(ctx, request)
}

func (c *RawClient) exchange(ctx context.Context, request *dns.Msg) (answer *dns.Msg, err error) {
	if common.Done(ctx) {
		return nil, ctx.Err()
	}
	pack, err := request.Pack()
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	var (
		nn    int
		conn  net.Conn
		retry int
	)
	for {
		if retry > 1024 {
			panic("resolve: reached max retry,stop now! ")
		}

		conn, err = c.newUdpConn(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve: %w", err)
		}

		nn, err = conn.Write(pack[:])
		if err != nil {
			c.closeConn(conn)
			if errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
				retry++
				continue
			}
			return nil, err
		}

		if nn <= 0 {
			// wrong conn , close
			c.closeConn(conn)
			return nil, fmt.Errorf("resolve: conn return a zero  or negative count: %d", nn)
		}
		break
	}
	defer func() { c.conns <- conn }()

	var deadline time.Time
	if dead, ok := ctx.Deadline(); ok {
		if time.Now().After(deadline) {
			return nil, context.DeadlineExceeded
		}
		deadline = dead
	} else {
		deadline = time.Now().Add(constant.ResolverDefaultReadTimeout)
	}

	_ = conn.SetReadDeadline(deadline)
	readBuf := make([]byte, 4096)
	nn, err = conn.Read(readBuf[:])
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	readBuf = readBuf[:nn]
	answer = new(dns.Msg)
	err = answer.Unpack(readBuf)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}

	if answer == nil {
		panic("client return a nil dns message without error")
	}
	if answer.Rcode != dns.RcodeSuccess {
		return nil, RcodeError(answer.Rcode)
	}
	if answer.Id != request.Id {
		return nil, errors.New("incorrect id")
	}
	if answer.Truncated {
		return nil, errors.New("truncated")
	}

	return answer, nil
}

func (c *RawClient) closeConn(conn net.Conn) {
	_ = conn.Close()
	if c.connCount.Load() > 0 {
		c.connCount.Add(-1)
	}
}

func (c *RawClient) newUdpConn(ctx context.Context) (net.Conn, error) {
	select {
	case conn := <-c.conns:
		return conn, nil
	default:
		if c.connCount.Load() >= maxConn {
			// wait until available
			return <-c.conns, nil
		}

		// new one conn
		conn, err := c.dialer.DialContext(ctx, "udp", c.destination)
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
}
