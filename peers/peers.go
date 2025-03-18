package peers

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/gookit/slog"
)

type Peer struct {
	IP   net.IP
	Port uint16
}

func Unmarshal(peersBin []byte) ([]Peer, error) {
	const peerSize = 6 // 4 for IP, 2 for port

	if len(peersBin)%peerSize != 0 {
		slog.Error("Invalid peers data")
		return nil, errors.New("invalid peers data")
	}

	numPeers := len(peersBin) / peerSize
	peers := make([]Peer, numPeers)

	for i := 0; i < numPeers; i++ {
		offset := i * peerSize
		peers[i].IP = net.IPv4(
			peersBin[offset],
			peersBin[offset+1],
			peersBin[offset+2],
			peersBin[offset+3],
		)
		peers[i].Port = binary.BigEndian.Uint16(peersBin[offset+4 : offset+6])
	}
	return peers, nil
}

func (p Peer) String() string {
	return fmt.Sprintf("%s:%d", p.IP.String(), p.Port)
}
