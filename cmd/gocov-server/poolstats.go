package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// poolStatsInterval is how often the database pool's counters are logged.
const poolStatsInterval = time.Minute

// poolSample is the part of pgxpool.Stat the log reports: the gauges as
// they stand and the cumulative counters, which the log turns into what
// moved since the previous sample.
type poolSample struct {
	acquired, idle, total, max int32
	acquires, emptyAcquires    int64
	canceledAcquires           int64
	emptyAcquireWait           time.Duration
}

func samplePool(s *pgxpool.Stat) poolSample {
	return poolSample{
		acquired:         s.AcquiredConns(),
		idle:             s.IdleConns(),
		total:            s.TotalConns(),
		max:              s.MaxConns(),
		acquires:         s.AcquireCount(),
		emptyAcquires:    s.EmptyAcquireCount(),
		canceledAcquires: s.CanceledAcquireCount(),
		emptyAcquireWait: s.EmptyAcquireWaitTime(),
	}
}

// poolStatsAttrs describes cur against prev: the gauges as they are now,
// the counters as deltas. empty_acquires is the one to watch — each is a
// request that found no idle connection and had to wait for one, and
// empty_acquire_wait_ms is how long those waits added up to. ok is false
// when no connection was acquired since prev, so an idle instance logs
// nothing.
func poolStatsAttrs(prev, cur poolSample) (attrs []any, ok bool) {
	acquires := cur.acquires - prev.acquires
	if acquires == 0 && cur.acquired == 0 {
		return nil, false
	}
	return []any{
		"acquired", cur.acquired,
		"idle", cur.idle,
		"total", cur.total,
		"max", cur.max,
		"acquires", acquires,
		"empty_acquires", cur.emptyAcquires - prev.emptyAcquires,
		"empty_acquire_wait_ms", (cur.emptyAcquireWait - prev.emptyAcquireWait).Milliseconds(),
		"canceled_acquires", cur.canceledAcquires - prev.canceledAcquires,
	}, true
}

// logPoolStats logs the pool's size once, then its counters every
// interval until ctx is done. It is the only view of whether requests
// wait for a database connection: the pool is shared by uploads, the UI's
// reads and the blobstore, and the upload path holds a connection across
// forge calls.
func logPoolStats(ctx context.Context, log *slog.Logger, pool *pgxpool.Pool, interval time.Duration) {
	prev := samplePool(pool.Stat())
	log.Info("db pool", "max_conns", prev.max)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cur := samplePool(pool.Stat())
		if attrs, ok := poolStatsAttrs(prev, cur); ok {
			log.Info("db pool stats", attrs...)
		}
		prev = cur
	}
}
