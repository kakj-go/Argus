package chstats

import (
	"context"
	"sync/atomic"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Tracker accumulates ClickHouse progress packets for every storage query
// performed while evaluating one language-level query.
type Tracker struct {
	rows  atomic.Uint64
	bytes atomic.Uint64
}

func (tracker *Tracker) Context(ctx context.Context, settings clickhouse.Settings) context.Context {
	return clickhouse.Context(ctx,
		clickhouse.WithSettings(settings),
		clickhouse.WithProgress(tracker.record),
	)
}

func (tracker *Tracker) record(progress *clickhouse.Progress) {
	if progress == nil {
		return
	}
	tracker.rows.Add(progress.Rows)
	tracker.bytes.Add(progress.Bytes)
}

func (tracker *Tracker) Rows() int64 {
	return saturatingInt64(tracker.rows.Load())
}

func (tracker *Tracker) Bytes() int64 {
	return saturatingInt64(tracker.bytes.Load())
}

func saturatingInt64(value uint64) int64 {
	const maxInt64 = uint64(^uint64(0) >> 1)
	if value > maxInt64 {
		return int64(maxInt64)
	}
	return int64(value)
}
