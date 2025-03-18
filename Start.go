package main

import (
	"log"
	"os"

	torrentfile "github.com/Bhavyyadav25/torrent/torrentFile"
	"github.com/gookit/slog"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("Usage: ./torrent-client <torrent-file> <output-file>")
	}

	tf, err := torrentfile.Open(os.Args[1])
	if err != nil {
		slog.Error("Failed to open torrent file", err)
		log.Fatal(err)
	}

	err = tf.DownloadToFile(os.Args[2])
	if err != nil {
		slog.Error("Failed to download torrent", err)
		log.Fatal(err)
	}
}
