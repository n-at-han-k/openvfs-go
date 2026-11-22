package directory

import (
	"context"
	"log"
)

type SyncRequest struct {
	Cause Event
}

type Syncer struct {
	scanner  *Scanner
	requests chan SyncRequest
	onChange func(Event)
}

func NewSyncer(root string, ignored func(string) bool, onChange func(Event)) *Syncer {
	return &Syncer{
		scanner:  NewScanner(root, ignored),
		requests: make(chan SyncRequest, 1),
		onChange: onChange,
	}
}

func (s *Syncer) Request(req SyncRequest) bool {
	select {
	case s.requests <- req:
		return true
	default:
		return false
	}
}

func (s *Syncer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.requests:
			s.sync(ctx, req)
		}
	}
}

func (s *Syncer) sync(ctx context.Context, req SyncRequest) {
	err := s.scanner.Scan(func(entry Event) {
		select {
		case <-ctx.Done():
		default:
			s.onChange(entry)
		}
	})
	if err != nil {
		log.Printf("sync (%v): local discovery failed: %v", req.Cause.Kind, err)
		return
	}
}
