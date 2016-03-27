package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"euphoria.io/heim-client/client"
	"euphoria.io/heim/proto"
	"euphoria.io/scope"
)

var (
	cookieJar = flag.String("cookies", "~/.heim/cookies", "file to save encrypted cookies in")
	server    = flag.String("server", "https://euphoria.io", "base URL of heim server")
)

func init() {
	flag.Usage = usage
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [options] ROOM\n\n", filepath.Base(os.Args[0]))
	flag.PrintDefaults()
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}

	if err := run(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

type sendReceiver struct{}

func (sendReceiver) SendEvent(event *proto.SendEvent) error {
	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("%s (%s) [%s] %s\n", timestamp, event.Sender.ID, event.Sender.Name, event.Content)
	return nil
}

func run(room string) error {
	ctx := scope.New()
	hc, err := client.DialRoom(ctx, *server, room)
	if err != nil {
		return err
	}
	defer hc.Close()

	hc.Add(&client.Presence{})
	hc.Add(&sendReceiver{})

	for {
		select {}
	}

	return nil
}
