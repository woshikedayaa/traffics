package resolver

import (
	"context"
	"errors"
	"fmt"
	"github.com/miekg/dns"
	"math/rand/v2"
	"net"
	"net/netip"
)

type Strategy uint8

const (
	StrategyDefault    Strategy = iota
	StrategyPreferIPv4          // "prefer_ipv4"
	StrategyPreferIPv6          // "prefer_ipv6"
	StrategyIPv4Only            // "ipv4_only"
	StrategyIPv6Only            // "ipv6_only"
	strategyMax
)

func (s Strategy) String() string {
	switch s {
	case StrategyPreferIPv4:
		return "prefer_ipv4"
	case StrategyPreferIPv6:
		return "prefer_ipv6"
	case StrategyIPv4Only:
		return "ipv4_only"
	case StrategyIPv6Only:
		return "ipv6_only"
	case StrategyDefault:
		return ""
	default:
		return fmt.Sprintf("strategy: %d", uint8(s))
	}
}

func (s Strategy) IsValid() bool {
	return s < strategyMax
}

func ParseStrategy(s string) (Strategy, bool) {
	switch s {
	case "prefer_ipv4":
		return StrategyPreferIPv4, true
	case "prefer_ipv6":
		return StrategyPreferIPv6, true
	case "ipv4_only":
		return StrategyIPv4Only, true
	case "ipv6_only":
		return StrategyIPv6Only, true
	case "":
		return StrategyDefault, true
	default:
		return StrategyDefault, false
	}
}

type Resolver interface {
	Lookup(ctx context.Context, fqdn string, strategy Strategy) (A []netip.Addr, AAAA []netip.Addr, err error)
}

type Exchanger interface {
	Exchange(ctx context.Context, msg *dns.Msg) (answer *dns.Msg, err error)
}

type DNSClient interface {
	Resolver
	Exchanger
}

type SystemResolver struct {
}

func NewSystemResolver() *SystemResolver {
	return &SystemResolver{}
}

func (s *SystemResolver) Lookup(ctx context.Context, fqdn string, strategy Strategy) (A []netip.Addr, AAAA []netip.Addr, err error) {
	var errStrategyUnknown = errors.New("network: unknown dns strategy")

	if !strategy.IsValid() {
		return nil, nil, errStrategyUnknown
	}
	fqdn = dns.Fqdn(fqdn)
	answer, err := net.DefaultResolver.LookupIPAddr(ctx, fqdn)
	if err != nil {
		return nil, nil, err
	}
	for _, addr := range answer {
		netipip, ok := netip.AddrFromSlice(addr.IP)
		if !ok || !netipip.IsValid() {
			continue
		}

		if strategy != StrategyIPv6Only && netipip.Is4() {
			A = append(A, netipip)
		}
		if strategy != StrategyIPv4Only && netipip.Is6() {
			AAAA = append(AAAA, netipip)
		}
	}
	return randomSortAddresses(A), randomSortAddresses(AAAA), nil
}

func MessageToAddresses(response *dns.Msg) (address []netip.Addr, err error) {
	if response.Rcode != dns.RcodeSuccess {
		return nil, RcodeError(response.Rcode)
	}
	for _, rawAnswer := range response.Answer {
		switch answer := rawAnswer.(type) {
		case *dns.A:
			a, ok := netip.AddrFromSlice(answer.A)
			if !ok {
				continue
			}
			address = append(address, a)
		case *dns.AAAA:
			aaaa, ok := netip.AddrFromSlice(answer.AAAA)
			if !ok {
				continue
			}
			address = append(address, aaaa)
		default:
			// discard others
		}
	}
	return
}

func FilterAddress(A []netip.Addr, AAAA []netip.Addr, strategy Strategy) (
	[]netip.Addr, []netip.Addr) {
	if strategy == StrategyIPv4Only {
		return A, nil
	}
	if strategy == StrategyIPv6Only {
		return nil, AAAA
	}
	return A, AAAA
}

func FqdnToQuestion(fqdn string, strategy Strategy) []dns.Question {
	basic := dns.Question{
		Name:   fqdn,
		Qclass: dns.ClassINET,
	}
	switch strategy {
	case StrategyIPv6Only:
		return []dns.Question{{
			Name:   fqdn,
			Qclass: dns.ClassINET,
			Qtype:  dns.TypeAAAA,
		}}
	case StrategyIPv4Only:
		return []dns.Question{{
			Name:   fqdn,
			Qclass: dns.ClassINET,
			Qtype:  dns.TypeA,
		}}
	default:
		q4, q6 := basic, basic
		q4.Qtype = dns.TypeA
		q6.Qtype = dns.TypeAAAA
		return []dns.Question{
			q4, q6,
		}
	}
}

func randomSortAddresses(raw []netip.Addr) []netip.Addr {
	if len(raw) <= 1 {
		return raw
	}
	var copied []netip.Addr
	copy(copied, raw)
	rand.Shuffle(len(copied), func(i, j int) {
		copied[i], copied[j] = copied[j], copied[i]
	})
	return copied
}

type RcodeError int

func (e RcodeError) Error() string {
	return fmt.Sprintf("resolve: server return rcode %s", dns.RcodeToString[int(e)])
}
