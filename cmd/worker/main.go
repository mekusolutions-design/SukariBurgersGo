package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/restflow/api-go/internal/config"
	"github.com/restflow/api-go/internal/eventstore"
	"github.com/restflow/api-go/internal/repository"
	"github.com/restflow/api-go/internal/worker"
	"github.com/restflow/api-go/pkg/logging"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		// Worker does not require JWT_SECRET in production for DB-only jobs
		if strings.Contains(err.Error(), "JWT_SECRET") {
			_ = os.Setenv("JWT_SECRET", "worker-not-used")
			cfg, err = config.Load()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "config: %v\n", err)
			os.Exit(1)
		}
	}

	log := logging.New(cfg.Env)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := repository.NewWorkerPool(ctx, cfg)
	if err != nil {
		log.Error("db", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	es := eventstore.NewStore(pool, log)
	runner := worker.NewRunner(pool, es, log)
	if err := runner.EnsureSchema(ctx); err != nil {
		log.Error("schema", "error", err)
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "health":
		if err := pool.Ping(ctx); err != nil {
			fmt.Println("unhealthy:", err)
			os.Exit(1)
		}
		fmt.Println("ok")

	case "run":
		// Continuous loop — Render Background Worker default
		runner.Loop(ctx)

	case "run-job":
		if len(os.Args) < 3 {
			fmt.Println("usage: restflow-worker run-job <job-id>")
			os.Exit(1)
		}
		if err := runner.RunOne(ctx, os.Args[2]); err != nil {
			log.Error("run-job", "error", err)
			os.Exit(1)
		}
		fmt.Println("ok")

	case "enqueue":
		// restflow-worker enqueue <type> [--shop-id=1] [--payload='{}']
		if len(os.Args) < 3 {
			fmt.Println("usage: restflow-worker enqueue <type> [--shop-id=1] [--key=value ...]")
			fmt.Println("types:", worker.TypeFullProjectionRebuild, worker.TypeReportExport,
				worker.TypeNightlyReconcileBackfill, worker.TypeBulkRecomputeVarianceConsume)
			os.Exit(1)
		}
		jobType := os.Args[2]
		payload := map[string]interface{}{}
		var shopID *string
		for _, a := range os.Args[3:] {
			if strings.HasPrefix(a, "--shop-id=") {
				s := strings.TrimPrefix(a, "--shop-id=")
				shopID = &s
				payload["shop_id"] = s
				continue
			}
			if strings.HasPrefix(a, "--") {
				kv := strings.TrimPrefix(a, "--")
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 2 {
					if parts[1] == "true" {
						payload[parts[0]] = true
					} else if parts[1] == "false" {
						payload[parts[0]] = false
					} else {
						payload[parts[0]] = parts[1]
					}
				}
			}
		}
		job, err := runner.Jobs().Enqueue(ctx, jobType, shopID, payload, strPtr("cli"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "enqueue: %v\n", err)
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(map[string]interface{}{
			"id": job.ID, "type": job.Type, "shop_id": shopID, "status": job.Status, "payload": payload,
		}, "", "  ")
		fmt.Println(string(b))

	case "status":
		if len(os.Args) < 3 {
			fmt.Println("usage: restflow-worker status <job-id>")
			os.Exit(1)
		}
		job, err := runner.Jobs().Get(ctx, os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "status: %v\n", err)
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(job, "", "  ")
		fmt.Println(string(b))

	case "retry":
		if len(os.Args) < 3 {
			fmt.Println("usage: restflow-worker retry <job-id>")
			os.Exit(1)
		}
		if err := runner.Jobs().Requeue(ctx, os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "retry: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("requeued")

	case "projections-rebuild":
		// Convenience: enqueue + run immediately
		shopID := "1"
		if len(os.Args) > 2 {
			shopID = os.Args[2]
		}
		job, err := runner.Jobs().Enqueue(ctx, worker.TypeFullProjectionRebuild, &shopID, map[string]interface{}{
			"shop_id": shopID,
		}, strPtr("cli"))
		if err != nil {
			log.Error("enqueue", "error", err)
			os.Exit(1)
		}
		log.Info("running rebuild", "job_id", job.ID, "shop", shopID)
		jobCtx, cancel := context.WithTimeout(ctx, 6*time.Hour)
		defer cancel()
		if err := runner.RunOne(jobCtx, job.ID); err != nil {
			log.Error("rebuild", "error", err)
			os.Exit(1)
		}
		fmt.Println("rebuild complete", job.ID)

	default:
		fmt.Println("unknown command:", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`restflow-worker — RestFlow background jobs

commands:
  health                         Ping database
  run                            Poll & execute queued jobs (Render worker)
  run-job <job-id>               Run one job by id
  enqueue <type> [--shop-id=1] [--key=value]
  status <job-id>
  retry <job-id>
  projections-rebuild [shop_id]  Enqueue+run full_projection_rebuild

job types:
  full_projection_rebuild
  report_export
  nightly_reconcile_backfill
  bulk_recompute_variance_consumption

examples:
  restflow-worker enqueue full_projection_rebuild --shop-id=1
  restflow-worker enqueue report_export --shop-id=1 --report_type=sales_summary --from=2026-09-01 --to=2026-09-22
  restflow-worker enqueue nightly_reconcile_backfill --shop-id=1 --mode=both --date=2026-09-21
  restflow-worker enqueue bulk_recompute_variance_consumption --shop-id=1 --scope=both --from=2026-09-01 --to=2026-09-22
  restflow-worker run`)
}

func strPtr(s string) *string { return &s }
