package chstats

import (
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
)

func TestTrackerAccumulatesProgress(t *testing.T) {
	tracker := &Tracker{}
	tracker.record(&clickhouse.Progress{Rows: 3, Bytes: 5})
	tracker.record(&clickhouse.Progress{Rows: 7, Bytes: 11})
	if tracker.Rows() != 10 || tracker.Bytes() != 16 {
		t.Fatalf("unexpected progress rows=%d bytes=%d", tracker.Rows(), tracker.Bytes())
	}
}
