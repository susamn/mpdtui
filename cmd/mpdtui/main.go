// Command mpdtui is a lazygit-style terminal UI for MPD.
package main

import (
	"flag"
	"fmt"
	"os"

	"mpdtui/internal/config"
	"mpdtui/internal/mini"
	"mpdtui/internal/mpdclient"
	"mpdtui/internal/ui"
)

func main() {
	miniMode := flag.Bool("mini", false, "run the lightweight inline player instead of the full panel UI")
	flag.Parse()

	cfg := config.Load()
	client, err := mpdclient.Dial(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mpdtui: connect to MPD at %s: %v\n", cfg.Addr(), err)
		os.Exit(1)
	}
	defer client.Close()

	if *miniMode {
		err = mini.Run(client)
	} else {
		err = ui.Run(client)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "mpdtui: %v\n", err)
		os.Exit(1)
	}
}
