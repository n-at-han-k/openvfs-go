package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	apiReady := make(chan error, 1)
	go func() {
		if err := serveAPI(ctx, socketPath, apiReady); err != nil {
			log.Printf("socket api: %v", err)
		}
	}()
	if err := <-apiReady; err != nil {
		log.Fatalf("socket api: %v", err)
	}

	ready := make(chan error, 1)
	go func() {
		if err := runBackend(ctx, root, "openvfs:local", socketPath, ready); err != nil {
			log.Printf("openvfs: %v", err)
		}
	}()
	if err := <-ready; err != nil {
		log.Fatalf("openvfs: %v", err)
	}

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
			if event.Kind == directory.EventPoll {
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
			case directory.EventCreated:
				log.Printf("created %s", event.Path)
			case directory.EventModified:
				log.Printf("modified %s", event.Path)
			case directory.EventDeleted:
				log.Printf("deleted %s", event.Path)
			case directory.EventRenamed:
				log.Printf("renamed %s", event.Path)
			case directory.EventRescan:
				log.Print("events lost, full rescan")
			}
		}
	}
}
