package constant

import "strings"

type Protocol string

const (
	ProtocolTCP    Protocol = "tcp"
	ProtocolUDP    Protocol = "udp"
	ProtocolIP     Protocol = "ip"
	ProtocolTCPUDP Protocol = "tcp+udp"
)

func (p Protocol) ToProtocolList() ProtocolList {
	switch p {
	case ProtocolTCP:
		return []string{"tcp"}
	case ProtocolUDP:
		return []string{"udp"}
	case ProtocolTCPUDP, "":
		return []string{"tcp", "udp"}
	case ProtocolIP:
		return []string{"ip"}
	default:
		return nil
	}
}

func ParseProtocol(name string) Protocol {
	if len(name) == 0 {
		return ""
	}

	switch Protocol(name) {
	case ProtocolTCP:
		return ProtocolTCP
	case ProtocolUDP:
		return ProtocolUDP
	case ProtocolIP:
		return ProtocolIP
	default:
		multi := strings.Split(name, "+")
		if len(multi) == 2 {
			if multi[0] == "tcp" && multi[1] == "udp" ||
				multi[0] == "udp" && multi[1] == "tcp" {
				return ProtocolTCPUDP
			}
		}
		return ""
	}
}
