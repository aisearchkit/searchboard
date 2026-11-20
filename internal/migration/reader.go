package migration

import "context"

// CursorStrategy is chosen per source cluster version (design doc section 7.2).
type CursorStrategy string

const (
	StrategyPITSearchAfter CursorStrategy = "pit_search_after" // ES 7.10+/8+/OpenSearch 2.4+
	StrategySearchAfter    CursorStrategy = "search_after"     // ES 6.8 - 7.9 (range-segmented)
	StrategyScroll         CursorStrategy = "scroll"           // ES 5.x fallback, tightly bounded
)

// Checkpoint is persisted atomically for exact resume after restart.
type Checkpoint struct {
	SliceID        int
	LastSortValues []any
	DiskOffset     int64
	BulkAckedSeq   uint64
}

// Reader streams documents of one slice into the disk queue.
type Reader interface {
	Run(ctx context.Context, slice int, out DiskQueue) error
}

// DiskQueue decouples read and write speed (Pebble / segmented WAL).
type DiskQueue interface {
	Append(ctx context.Context, docs [][]byte) (offset int64, err error)
	Read(ctx context.Context, from int64, maxDocs int) (docs [][]byte, next int64, err error)
	Ack(ctx context.Context, upTo int64) error
	DeadLetter(ctx context.Context, doc []byte, reason string) error
	Close() error
}
