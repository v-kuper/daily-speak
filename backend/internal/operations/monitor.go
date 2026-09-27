package operations

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/workqueue"
)

var ErrUnavailable = errors.New("operations data unavailable")

type Monitor struct {
	database *db.DB
	jobs     *workqueue.Store
}

type Readiness struct {
	Ready  bool
	Checks map[string]string
}

type Snapshot struct {
	Database DatabaseSnapshot
	Queues   []QueueSnapshot
}

func NewMonitor(database *db.DB, jobs *workqueue.Store) *Monitor {
	return &Monitor{database: database, jobs: jobs}
}

func (monitor *Monitor) CheckReadiness(ctx context.Context, maxQueueDepth int, maxOldestJob time.Duration) Readiness {
	result := Readiness{Ready: true, Checks: map[string]string{"database": "ok", "queue": "ok"}}
	if monitor == nil || monitor.database == nil || monitor.database.Ping(ctx) != nil {
		result.Ready = false
		result.Checks["database"] = "unavailable"
		return result
	}
	if monitor.jobs == nil {
		result.Ready = false
		result.Checks["queue"] = "unavailable"
		return result
	}
	pressure, err := monitor.jobs.Pressure(ctx)
	switch {
	case err != nil:
		result.Ready = false
		result.Checks["queue"] = "unavailable"
	case maxQueueDepth > 0 && pressure.ActiveCount > int64(maxQueueDepth):
		result.Ready = false
		result.Checks["queue"] = "depth_exceeded"
	case maxOldestJob > 0 && pressure.OldestAgeSeconds > maxOldestJob.Seconds():
		result.Ready = false
		result.Checks["queue"] = "oldest_job_exceeded"
	}
	return result
}

func (monitor *Monitor) Snapshot(ctx context.Context) (Snapshot, error) {
	if monitor == nil || monitor.database == nil || monitor.jobs == nil {
		return Snapshot{}, ErrUnavailable
	}
	stats, err := monitor.jobs.Stats(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	pool := monitor.database.PoolStats()
	queues := make([]QueueSnapshot, 0, len(stats))
	for _, stat := range stats {
		queues = append(queues, QueueSnapshot{
			Kind: stat.Kind, State: stat.State, Count: stat.Count,
			OldestAgeSeconds: stat.OldestAgeSeconds, RecentTerminal: stat.RecentTerminal,
		})
	}
	return Snapshot{
		Database: DatabaseSnapshot{
			TotalConns: pool.TotalConns, IdleConns: pool.IdleConns,
			AcquiredConns: pool.AcquiredConns, MaxConns: pool.MaxConns,
		},
		Queues: queues,
	}, nil
}
