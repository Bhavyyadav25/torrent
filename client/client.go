package client

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Bhavyyadav25/torrent/peers"
	"github.com/gookit/slog"
)

type Bitfield []byte

type Client struct {
	Conn     net.Conn
	Choked   bool
	Bitfield Bitfield
	peer     peers.Peer
	infoHash [20]byte
	peerID   [20]byte
}

type Handshake struct {
	Pstr     string
	InfoHash [20]byte
	PeerID   [20]byte
}

type messageID uint8

const (
	MsgChoke         messageID = 0
	MsgUnchoke       messageID = 1
	MsgInterested    messageID = 2
	MsgNotInterested messageID = 3
	MsgHave          messageID = 4
	MsgBitfield      messageID = 5
	MsgRequest       messageID = 6
	MsgPiece         messageID = 7
	MsgCancel        messageID = 8
)

type Message struct {
	ID      messageID
	Payload []byte
}

func New(peer peers.Peer, peerID, infoHash [20]byte) (*Client, error) {
	conn, err := net.DialTimeout("tcp", peer.String(), 3*time.Second)
	if err != nil {
		slog.Error("Failed to connect to peer", err)
		return nil, err
	}

	err = completeHandshake(conn, infoHash, peerID)
	if err != nil {
		conn.Close()
		slog.Error("Failed to complete handshake with peer", err)
		return nil, err
	}

	bf, err := recvBitfield(conn)
	if err != nil {
		conn.Close()
		slog.Error("Failed to receive bitfield from peer", err)
		return nil, err
	}

	return &Client{
		Conn:     conn,
		Bitfield: bf,
		Choked:   true,
		peer:     peer,
		infoHash: infoHash,
		peerID:   peerID,
	}, nil
}

func completeHandshake(conn net.Conn, infoHash, peerID [20]byte) error {
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		slog.Error("Failed to set deadline", err)
		return err
	}
	defer conn.SetDeadline(time.Time{})

	req := Handshake{
		Pstr:     "BitTorrent protocol",
		InfoHash: infoHash,
		PeerID:   peerID,
	}

	_, err := conn.Write(req.Serialize())
	if err != nil {
		slog.Error("Failed to write handshake", err)
		return err
	}

	res, err := ReadHandshake(conn)
	if err != nil {
		slog.Error("Failed to read handshake", err)
		return err
	}

	if !bytes.Equal(res.InfoHash[:], infoHash[:]) {
		slog.Error("Infohash mismatch")
		return errors.New("infohash mismatch")
	}
	return nil
}

func (h *Handshake) Serialize() []byte {
	buf := make([]byte, len(h.Pstr)+49)
	buf[0] = byte(len(h.Pstr))
	curr := 1
	curr += copy(buf[curr:], h.Pstr)
	curr += copy(buf[curr:], make([]byte, 8)) // 8 reserved bytes
	curr += copy(buf[curr:], h.InfoHash[:])
	curr += copy(buf[curr:], h.PeerID[:])
	return buf
}

func (bf Bitfield) HasPiece(index int) bool {
	byteIndex := index / 8
	offset := index % 8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return false
	}
	return bf[byteIndex]>>(7-offset)&1 != 0
}

func (bf Bitfield) SetPiece(index int) {
	byteIndex := index / 8
	offset := index % 8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return
	}
	bf[byteIndex] |= 1 << (7 - offset)
}

func recvBitfield(conn net.Conn) (Bitfield, error) {
	err := conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		slog.Error("Failed to set deadline", err)
		return nil, err
	}
	defer conn.SetDeadline(time.Time{})

	msg, err := Read(conn)
	if err != nil {
		slog.Error("Failed to read bitfield", err)
		return nil, err
	}
	if msg == nil || msg.ID != MsgBitfield {
		slog.Error("Expected bitfield message")
		return nil, fmt.Errorf("expected bitfield message")
	}
	return Bitfield(msg.Payload), nil
}

func ReadHandshake(r io.Reader) (*Handshake, error) {
	lengthBuf := make([]byte, 1)
	_, err := io.ReadFull(r, lengthBuf)
	if err != nil {
		slog.Error("Failed to read pstrlen", err)
		return nil, err
	}
	pstrlen := int(lengthBuf[0])
	if pstrlen == 0 {
		slog.Error("pstrlen cannot be 0")
		return nil, fmt.Errorf("pstrlen cannot be 0")
	}

	handshakeBuf := make([]byte, 48+pstrlen)
	_, err = io.ReadFull(r, handshakeBuf)
	if err != nil {
		slog.Error("Failed to read handshake", err)
		return nil, err
	}

	var infoHash, peerID [20]byte

	copy(infoHash[:], handshakeBuf[pstrlen+8:pstrlen+28])
	copy(peerID[:], handshakeBuf[pstrlen+28:pstrlen+48])

	return &Handshake{
		Pstr:     string(handshakeBuf[0:pstrlen]),
		InfoHash: infoHash,
		PeerID:   peerID,
	}, nil
}

func Read(r io.Reader) (*Message, error) {
	lengthBuf := make([]byte, 4)
	_, err := io.ReadFull(r, lengthBuf)
	if err != nil {
		slog.Error("Failed to read length", err)
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBuf)

	if length == 0 {
		slog.Debug("Keep-alive")
		return nil, nil // keep-alive
	}

	messageBuf := make([]byte, length)
	_, err = io.ReadFull(r, messageBuf)
	if err != nil {
		return nil, err
	}

	return &Message{
		ID:      messageID(messageBuf[0]),
		Payload: messageBuf[1:],
	}, nil
}

func (c *Client) SendUnchoke() error {
	msg := Message{ID: MsgUnchoke}
	_, err := c.Conn.Write(msg.Serialize())
	if err != nil {
		slog.Error("Failed to send unchoke", err)
	}
	return err
}

func (c *Client) SendInterested() error {
	msg := Message{ID: MsgInterested}
	_, err := c.Conn.Write(msg.Serialize())
	if err != nil {
		slog.Error("Failed to send interested", err)
	}
	return err
}

func (c *Client) SendHave(index int) error {
	msg := Message{
		ID:      MsgHave,
		Payload: make([]byte, 4),
	}
	binary.BigEndian.PutUint32(msg.Payload, uint32(index))
	_, err := c.Conn.Write(msg.Serialize())
	if err != nil {
		slog.Error("Failed to send have", err)
	}
	return err
}

func (c *Client) SendRequest(index, begin, length int) error {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))

	msg := Message{
		ID:      MsgRequest,
		Payload: payload,
	}
	_, err := c.Conn.Write(msg.Serialize())
	if err != nil {
		slog.Error("Failed to send request", err)
	}
	return err
}

func (m *Message) Serialize() []byte {
	if m == nil {
		return make([]byte, 4)
	}
	length := uint32(len(m.Payload) + 1)
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[0:4], length)
	buf[4] = byte(m.ID)
	copy(buf[5:], m.Payload)
	return buf
}
func ParseHave(msg *Message) (int, error) {
	if msg.ID != MsgHave {
		slog.Errorf("Expected HAVE message, got ID %d", msg.ID)
		return 0, fmt.Errorf("expected HAVE message, got ID %d", msg.ID)
	}
	if len(msg.Payload) != 4 {
		slog.Errorf("Expected payload length 4, got %d", len(msg.Payload))
		return 0, fmt.Errorf("expected payload length 4, got %d", len(msg.Payload))
	}
	index := binary.BigEndian.Uint32(msg.Payload)
	return int(index), nil
}

// ParsePiece parses PIECE message
func ParsePiece(index int, buf []byte, msg *Message) (int, error) {
	if msg.ID != MsgPiece {
		slog.Errorf("Expected PIECE message, got ID %d", msg.ID)
		return 0, fmt.Errorf("expected PIECE message, got ID %d", msg.ID)
	}
	if len(msg.Payload) < 8 {
		slog.Errorf("Payload too short: %d < 8", len(msg.Payload))
		return 0, fmt.Errorf("payload too short: %d < 8", len(msg.Payload))
	}

	parsedIndex := binary.BigEndian.Uint32(msg.Payload[0:4])
	if int(parsedIndex) != index {
		slog.Errorf("Expected index %d, got %d", index, parsedIndex)
		return 0, fmt.Errorf("expected index %d, got %d", index, parsedIndex)
	}

	begin := binary.BigEndian.Uint32(msg.Payload[4:8])
	if begin >= uint32(len(buf)) {
		slog.Errorf("Begin offset too high: %d >= %d", begin, len(buf))
		return 0, fmt.Errorf("begin offset too high: %d >= %d", begin, len(buf))
	}

	data := msg.Payload[8:]
	if int(begin)+len(data) > len(buf) {
		slog.Errorf("Data too long [%d] for offset %d with buffer %d", len(data), begin, len(buf))
		return 0, fmt.Errorf("data too long [%d] for offset %d with buffer %d", len(data), begin, len(buf))
	}

	copy(buf[begin:], data)
	return len(data), nil
}

func (c *Client) Read() (*Message, error) {
	return Read(c.Conn)
}
