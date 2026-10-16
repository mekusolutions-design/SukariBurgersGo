package worker

import (
	"encoding/json"
	"time"
)

// Job type constants — keep stable for ops scripts.
const (
	TypeFullProjectionRebuild        = "full_projection_rebuild"
	TypeReportExport                 = "report_export"
	TypeNightlyReconcileBackfill     = "nightly_reconcile_backfill"
	TypeBulkRecomputeVarianceConsume = "bulk_recompute_variance_consumption"
	TypeExpirySweep                   = "expiry_sweep"
)

const (
	StatusQueued     = "queued"
	StatusRunning    = "running"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
)

// Job is a durable background work unit.
type Job struct {
	ID           string
	Type         string
	ShopID       *string
	Payload      map[string]interface{}
	Status       string
	Progress     map[string]interface{}
	Result       map[string]interface{}
	ErrorMessage *string
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	CreatedBy    *string
	Attempts     int
}

func (j *Job) PayloadString(key, fallback string) string {
	if j.Payload == nil {
		return fallback
	}
	if v, ok := j.Payload[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func (j *Job) PayloadBool(key string, fallback bool) bool {
	if j.Payload == nil {
		return fallback
	}
	if v, ok := j.Payload[key].(bool); ok {
		return v
	}
	return fallback
}

func (j *Job) PayloadFloat(key string, fallback float64) float64 {
	if j.Payload == nil {
		return fallback
	}
	switch v := j.Payload[key].(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return fallback
}
