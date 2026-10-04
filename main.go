package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	syncer "github.com/msoap/mac-photos-sync/internal/sync"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	library := flag.String("library", filepath.Join(home, "Pictures", "Photos Library.photoslibrary"), "Photos library path")
	dry := flag.Bool("dry-run", false, "show planned changes without writing")
	verbose := flag.Bool("verbose", false, "print detailed operations")
	rebuild := flag.Bool("rebuild-db", false, "rebuild the destination catalog")
	workers := flag.Int("workers", 4, "concurrent metadata workers")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: mac-photos-sync [options] <destination>\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 || *workers < 1 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, err = syncer.Run(ctx, syncer.Options{Library: *library, Destination: flag.Arg(0), DryRun: *dry, Verbose: *verbose, RebuildDB: *rebuild, Workers: *workers, Output: os.Stdout})
	if err != nil {
		fmt.Fprintln(os.Stderr, "mac-photos-sync:", err)
		os.Exit(1)
	}
}
