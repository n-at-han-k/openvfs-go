package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"example.com/project/packages/openvfs"
	"example.com/project/src/directory"
	"example.com/project/src/journal"
)

func main() {
	root := "/path/to/sync"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	dbPath := ".sync-journal.db"
	ignored := func(rel string) bool {
		return rel == dbPath || rel == dbPath+"-wal" || rel == dbPath+"-shm"
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	events := make(chan directory.Event, 256)

	journal, err := journal.Open(filepath.Join(root, dbPath))
	if err != nil {
		log.Fatal(err)
	}
	defer journal.Close()

	syncer := directory.NewSyncer(root, ignored, journal, func(event directory.Event) {
		select {
		case events <- event:
		case <-ctx.Done():
		}
	})
	go syncer.Run(ctx)

	socketPath := filepath.Join(os.TempDir(), "openvfs.sock")

	go openvfs.ServeAPI(ctx, socketPath, func(event directory.Event) {
		select {
		case events <- event:
		case <-ctx.Done():
		}
	})

	go openvfs.Run(ctx, root, "openvfs:local", socketPath, func(event directory.Event) {
		select {
		case events <- event:
		case <-ctx.Done():
		}
	})

	go directory.RunWatcher(ctx, root, ignored, func(event directory.Event) {
		select {
		case events <- event:
		case <-ctx.Done():
		}
	})

	go directory.RunPoller(ctx, 30*time.Second, func(event directory.Event) {
		select {
		case events <- event:
		case <-ctx.Done():
		}
	})

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			if event.Kind == directory.Poll {
				syncer.Request(directory.SyncRequest{Cause: event})
				continue
			}

			switch event.Kind {
			case directory.Create:
			case directory.Write:
			case directory.Remove:
			case directory.Rename:
			case directory.Chmod:
			case directory.LocalEntry:
			}
		}
	}
}
