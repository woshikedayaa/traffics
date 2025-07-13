package constant

import (
	"errors"
	"slices"
	"strconv"
)

type NetworkVersion int

const (
	NetworkVersion4    = 4
	NetworkVersion6    = 6
	NetworkVersionDual = 0
)

var (
	ErrInvalidNetwork = errors.New("dialer: invalid network")
)

type Network struct {
	Protocol Protocol
	Version  NetworkVersion
}

func ParseNetwork(network string) (Network, error) {
	nn := Network{}
	switch network {
	case string(ProtocolTCP), string(ProtocolUDP), string(ProtocolIP):
		nn.Version = NetworkVersionDual
		nn.Protocol = Protocol(network)
	case "tcp4", "udp4", "ip4":
		nn.Version = NetworkVersion4
		if len(network) == 3 { // => ip4
			nn.Protocol = ProtocolIP
		} else {
			nn.Protocol = Protocol(network[:3])
		}
	case "tcp6", "udp6", "ip6":
		nn.Version = NetworkVersion6
		if len(network) == 3 { // => ip6
			nn.Protocol = ProtocolIP
		} else {
			nn.Protocol = Protocol(network[:3])
		}
	default:
		return Network{}, ErrInvalidNetwork
	}
	return nn, nil
}

func (n Network) String() string {
	return string(n.Protocol) + strconv.Itoa(int(n.Version))
}

type ProtocolList []string

func (n ProtocolList) Contain(network string) bool {
	nn, err := ParseNetwork(network)
	if err != nil {
		return false
	}
	return slices.Contains(n, string(nn.Protocol))
}
