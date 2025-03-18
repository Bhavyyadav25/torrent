package torrentfile

import (
	"bytes"
	"crypto/sha1"
	"errors"

	"github.com/gookit/slog"
	"github.com/jackpal/bencode-go"
)

type bencodeInfo struct {
	Pieces      string `bencode:"pieces"`
	PieceLength int    `bencode:"piece length"`
	Length      int    `bencode:"length"`
	Name        string `bencode:"name"`
}

type bencodeTorrent struct {
	Announce string      `bencode:"announce"`
	Info     bencodeInfo `bencode:"info"`
}

func (bto *bencodeTorrent) toTorrentFile() (*TorrentFile, error) {
	infoHash, err := calculateInfoHash(bto.Info)
	if err != nil {
		slog.Error("Failed to calculate info hash", err)
		return nil, err
	}

	pieceHashes, err := splitPieceHashes(bto.Info.Pieces)
	if err != nil {
		slog.Error("Failed to split piece hashes", err)
		return nil, err
	}

	return &TorrentFile{
		Announce:    bto.Announce,
		InfoHash:    infoHash,
		PieceHashes: pieceHashes,
		PieceLength: bto.Info.PieceLength,
		Length:      bto.Info.Length,
		Name:        bto.Info.Name,
	}, nil
}

func calculateInfoHash(info bencodeInfo) ([20]byte, error) {
	var buf bytes.Buffer
	err := bencode.Marshal(&buf, info)
	if err != nil {
		slog.Error("Failed to marshal info", err)
		return [20]byte{}, err
	}
	return sha1.Sum(buf.Bytes()), nil
}

func splitPieceHashes(pieces string) ([][20]byte, error) {
	hashLen := 20 // Length of SHA-1 hash
	buf := []byte(pieces)

	if len(buf)%hashLen != 0 {
		slog.Error("Malformed pieces string")
		return nil, errors.New("malformed pieces string")
	}

	numHashes := len(buf) / hashLen
	hashes := make([][20]byte, numHashes)

	for i := 0; i < numHashes; i++ {
		copy(hashes[i][:], buf[i*hashLen:(i+1)*hashLen])
	}
	return hashes, nil
}
