package torrentfile

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Bhavyyadav25/torrent/client"
	"github.com/Bhavyyadav25/torrent/peers"
	"github.com/gookit/slog"
	"github.com/jackpal/bencode-go"
)

type TorrentFile struct {
	Announce    string
	InfoHash    [20]byte
	PieceHashes [][20]byte
	PieceLength int
	Length      int
	Name        string
}

type pieceProgress struct {
	index      int
	client     *client.Client
	buf        []byte
	downloaded int
	requested  int
	backlog    int
}

type Progress struct {
	TotalPieces int
	DonePieces  int32 // atomic
	PieceLength int
	TotalLength int
}

type pieceWork struct {
	index  int
	hash   [20]byte
	length int
}

type pieceResult struct {
	index int
	buf   []byte
}

type torrent struct {
	Peers       []peers.Peer
	PeerID      [20]byte
	InfoHash    [20]byte
	PieceHashes [][20]byte
	PieceLength int
	Length      int
	Name        string
}

func (p *Progress) Log(stop chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	slog.Println() // Initial newline for cleaner output

	for {
		select {
		case <-ticker.C:
			done := atomic.LoadInt32(&p.DonePieces)
			percent := float64(done) / float64(p.TotalPieces) * 100
			elapsed := time.Since(startTime).Seconds()

			bytesDownloaded := int64(done) * int64(p.PieceLength)
			if done == int32(p.TotalPieces) {
				bytesDownloaded = int64(p.TotalLength)
			}

			bytesPerSec := float64(bytesDownloaded) / elapsed
			totalMB := float64(p.TotalLength) / 1024 / 1024
			doneMB := float64(bytesDownloaded) / 1024 / 1024

			slog.Printf("\r\x1b[K") // Clear line
			slog.Printf(
				"Progress: %.2f%% | %d/%d pieces | %.2f MB/s | %.1fMB/%.1fMB",
				percent,
				done,
				p.TotalPieces,
				bytesPerSec/1024/1024,
				doneMB,
				totalMB,
			)
		case <-stop:
			slog.Println() // Final newline
			return
		}
	}
}

func (p *Progress) Percent() float64 {
	return float64(atomic.LoadInt32(&p.DonePieces)) / float64(p.TotalPieces) * 100
}

func Open(path string) (*TorrentFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	bto := bencodeTorrent{}
	err = bencode.Unmarshal(file, &bto)
	if err != nil {
		return nil, err
	}

	return bto.toTorrentFile()
}

func (t *TorrentFile) BuildTrackerURL(peerID [20]byte, port uint16) (string, error) {
	base, err := url.Parse(t.Announce)
	if err != nil {
		return "", err
	}

	params := url.Values{
		"info_hash":  []string{string(t.InfoHash[:])},
		"peer_id":    []string{string(peerID[:])},
		"port":       []string{strconv.Itoa(int(port))},
		"uploaded":   []string{"0"},
		"downloaded": []string{"0"},
		"compact":    []string{"1"},
		"left":       []string{strconv.Itoa(t.Length)},
	}

	base.RawQuery = params.Encode()
	return base.String(), nil
}

func (t *TorrentFile) DownloadToFile(path string) error {
	peerID := generatePeerID()
	peers, err := t.requestPeers(peerID, 6881)
	if err != nil {
		return err
	}

	torrent := &torrent{
		Peers:       peers,
		PeerID:      peerID,
		InfoHash:    t.InfoHash,
		PieceHashes: t.PieceHashes,
		PieceLength: t.PieceLength,
		Length:      t.Length,
		Name:        t.Name,
	}

	buf, err := torrent.Download()
	if err != nil {
		return err
	}

	outFile, err := os.Create(path)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = outFile.Write(buf)
	if err != nil {
		return err
	}
	return nil
}

func generatePeerID() [20]byte {
	var peerID [20]byte
	copy(peerID[:], "-GO0001-")
	rand.Read(peerID[8:])
	return peerID
}

func (t *TorrentFile) requestPeers(peerID [20]byte, port uint16) ([]peers.Peer, error) {
	url, err := t.BuildTrackerURL(peerID, port)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	trackerResp := struct {
		Interval int    `bencode:"interval"`
		Peers    string `bencode:"peers"`
	}{}

	err = bencode.Unmarshal(resp.Body, &trackerResp)
	if err != nil {
		return nil, err
	}

	return peers.Unmarshal([]byte(trackerResp.Peers))
}

func (t *torrent) Download() ([]byte, error) {
	progress := &Progress{
		TotalPieces: len(t.PieceHashes),
		PieceLength: t.PieceLength,
		TotalLength: t.Length,
	}

	stopLog := make(chan struct{})
	go progress.Log(stopLog)
	defer close(stopLog)

	workQueue := make(chan *pieceWork, len(t.PieceHashes))
	results := make(chan *pieceResult)

	for index, hash := range t.PieceHashes {
		length := t.calculatePieceSize(index)
		workQueue <- &pieceWork{index, hash, length}
	}

	for _, peer := range t.Peers {
		go t.startDownloadWorker(peer, workQueue, results)
	}

	buf := make([]byte, t.Length)
	donePieces := 0

	for donePieces < len(t.PieceHashes) {
		res := <-results
		begin, end := t.calculateBoundsForPiece(res.index)
		copy(buf[begin:end], res.buf)
		atomic.AddInt32(&progress.DonePieces, 1)
		donePieces++
	}
	close(workQueue)

	return buf, nil
}

func (t *torrent) calculatePieceSize(index int) int {
	begin := index * t.PieceLength
	end := begin + t.PieceLength
	if end > t.Length {
		return t.Length - begin
	}
	return t.PieceLength
}

func (t *torrent) calculateBoundsForPiece(index int) (begin int, end int) {
	begin = index * t.PieceLength
	end = begin + t.PieceLength
	if end > t.Length {
		end = t.Length
	}
	return
}

func (t *torrent) startDownloadWorker(peer peers.Peer, workQueue chan *pieceWork, results chan *pieceResult) {
	c, err := client.New(peer, t.PeerID, t.InfoHash)
	if err != nil {
		log.Printf("Could not connect to peer %s: %v\n", peer.IP, err)
		return
	}
	defer c.Conn.Close()

	c.SendUnchoke()
	c.SendInterested()

	for pw := range workQueue {
		if !c.Bitfield.HasPiece(pw.index) {
			workQueue <- pw
			continue
		}

		buf, err := attemptDownloadPiece(c, pw)
		if err != nil {
			log.Printf("Peer %s failed piece %d: %v\n", peer.IP, pw.index, err)
			workQueue <- pw
			return
		}

		err = checkIntegrity(pw, buf)
		if err != nil {
			log.Printf("Peer %s sent invalid piece %d\n", peer.IP, pw.index)
			workQueue <- pw
			continue
		}

		c.SendHave(pw.index)
		results <- &pieceResult{pw.index, buf}
	}
}

func attemptDownloadPiece(c *client.Client, pw *pieceWork) ([]byte, error) {
	state := pieceProgress{
		index:  pw.index,
		client: c,
		buf:    make([]byte, pw.length),
	}

	c.Conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer c.Conn.SetDeadline(time.Time{})

	for state.downloaded < pw.length {
		if !state.client.Choked {
			for state.backlog < 5 && state.requested < pw.length {
				blockSize := 16384 // 16KB
				if pw.length-state.requested < blockSize {
					blockSize = pw.length - state.requested
				}

				err := c.SendRequest(pw.index, state.requested, blockSize)
				if err != nil {
					return nil, err
				}
				state.backlog++
				state.requested += blockSize
			}
		}

		err := state.readMessage()
		if err != nil {
			return nil, err
		}
	}

	return state.buf, nil
}

func checkIntegrity(pw *pieceWork, buf []byte) error {
	hash := sha1.Sum(buf)
	if !bytes.Equal(hash[:], pw.hash[:]) {
		return fmt.Errorf("hash mismatch")
	}
	return nil
}

func (state *pieceProgress) readMessage() error {
	msg, err := state.client.Read()
	if err != nil {
		return err
	}

	if msg == nil { // Keep-alive
		return nil
	}

	switch msg.ID {
	case client.MsgUnchoke:
		state.client.Choked = false
	case client.MsgChoke:
		state.client.Choked = true
	case client.MsgHave:
		index, err := client.ParseHave(msg)
		if err != nil {
			return err
		}
		state.client.Bitfield.SetPiece(index)
	case client.MsgPiece:
		n, err := client.ParsePiece(state.index, state.buf, msg)
		if err != nil {
			return err
		}
		state.downloaded += n
		state.backlog--
	}
	return nil
}

func ParseHave(msg *client.Message) (int, error) {
	if msg.ID != client.MsgHave {
		return 0, fmt.Errorf("expected HAVE message")
	}
	if len(msg.Payload) != 4 {
		return 0, fmt.Errorf("invalid payload length")
	}
	return int(binary.BigEndian.Uint32(msg.Payload)), nil
}

func ParsePiece(index int, buf []byte, msg *client.Message) (int, error) {
	if msg.ID != client.MsgPiece {
		return 0, fmt.Errorf("expected PIECE message")
	}
	if len(msg.Payload) < 8 {
		return 0, fmt.Errorf("payload too short")
	}

	parsedIndex := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
	if parsedIndex != index {
		return 0, fmt.Errorf("unexpected index")
	}

	begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	if begin >= len(buf) {
		return 0, fmt.Errorf("begin offset too high")
	}

	data := msg.Payload[8:]
	if begin+len(data) > len(buf) {
		return 0, fmt.Errorf("data too long")
	}

	copy(buf[begin:], data)
	return len(data), nil
}
