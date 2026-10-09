package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/projections"
)

// Runner claims and executes jobs.
type Runner struct {
	jobs       *Store
	rebuild    *RebuildHandler
	report     *ReportHandler
	reconcile  *ReconcileHandler
	variance   *VarianceConsumptionHandler
	expiry     *ExpiryHandler
	log        *slog.Logger
	poll       time.Duration
	jobTimeout time.Duration
}

func NewRunner(pool *pgxpool.Pool, es *eventstore.Store, log *slog.Logger) *Runner {
	engine := projections.NewEngine(log)
	return &Runner{
		jobs:       NewStore(pool),
		rebuild:    NewRebuildHandler(pool, es, engine, log, 200),
		report:     NewReportHandler(pool, log),
		reconcile:  NewReconcileHandler(pool, es, log),
		variance:   NewVarianceConsumptionHandler(pool, log),
		expiry:     NewExpiryHandler(pool, es, log),
		log:        log,
		poll:       3 * time.Second,
		jobTimeout: 6 * time.Hour,
	}
}

func (r *Runner) EnsureSchema(ctx context.Context) error {
	return r.jobs.EnsureSchema(ctx)
}

func (r *Runner) Jobs() *Store { return r.jobs }

// Loop polls forever until ctx cancelled.
func (r *Runner) Loop(ctx context.Context) {
	r.log.Info("worker loop started", "poll", r.poll.String())
	for {
		select {
		case <-ctx.Done():
			r.log.Info("worker loop stopped")
			return
		default:
		}
		job, err := r.jobs.ClaimNext(ctx)
		if err != nil {
			r.log.Error("claim", "error", err)
			time.Sleep(r.poll)
			continue
		}
		if job == nil {
			time.Sleep(r.poll)
			continue
		}
		r.log.Info("job claimed", "job_id", job.ID, "type", job.Type)
		jobCtx, cancel := context.WithTimeout(ctx, r.jobTimeout)
		err = r.execute(jobCtx, job)
		cancel()
		if err != nil {
			r.log.Error("job failed", "job_id", job.ID, "error", err)
			_ = r.jobs.MarkFailed(ctx, job.ID, err.Error())
		} else {
			r.log.Info("job finished", "job_id", job.ID, "type", job.Type)
		}
	}
}

func (r *Runner) RunOne(ctx context.Context, jobID string) error {
	job, err := r.jobs.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status != StatusQueued && job.Status != StatusFailed {
		// allow re-run of failed; for queued claim is better
	}
	if job.Status == StatusQueued || job.Status == StatusFailed {
		if err := r.jobs.MarkRunning(ctx, job.ID); err != nil {
			return err
		}
		job.Status = StatusRunning
	}
	return r.execute(ctx, job)
}

func (r *Runner) execute(ctx context.Context, job *Job) error {
	switch job.Type {
	case TypeFullProjectionRebuild:
		return r.rebuild.Run(ctx, job, r.jobs)
	case TypeReportExport:
		return r.report.Run(ctx, job, r.jobs)
	case TypeNightlyReconcileBackfill:
		return r.reconcile.Run(ctx, job, r.jobs)
	case TypeBulkRecomputeVarianceConsume:
		return r.variance.Run(ctx, job, r.jobs)
	case TypeExpirySweep:
		return r.expiry.Run(ctx, job, r.jobs)
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}
}
