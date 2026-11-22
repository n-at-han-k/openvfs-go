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

	journal, err := directory.OpenJournal(filepath.Join(root, dbPath))
	if err != nil {
		log.Fatal(err)
	}
	defer journal.Close()

	scanner := directory.NewScanner(root, ignored, journal)

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
				found, err := scanner.Scan()
				if err != nil {
					log.Printf("scan failed: %v", err)
					continue
				}
				for _, change := range found {
					select {
					case events <- change:
					case <-ctx.Done():
						return
					}
				}
				continue
			}

			switch event.Kind {
			case directory.Create:
				// TODO: handle create
			case directory.Write:
				// TODO: handle write
			case directory.Remove:
				// TODO: handle removal
			case directory.Rename:
				// TODO: handle rename
			case directory.Chmod:
				// TODO: handle attribute change
			}
		}
	}
}
