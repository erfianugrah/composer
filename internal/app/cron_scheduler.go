package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/erfianugrah/composer/internal/domain/pipeline"
)

// CronScheduler runs pipeline triggers on a schedule.
type CronScheduler struct {
	pipelineSvc *PipelineService
	pipelines   pipeline.PipelineRepository
	logger      *zap.Logger
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	started     bool
}

func NewCronScheduler(pipelineSvc *PipelineService, pipelines pipeline.PipelineRepository, logger *zap.Logger) *CronScheduler {
	return &CronScheduler{
		pipelineSvc: pipelineSvc,
		pipelines:   pipelines,
		logger:      logger,
	}
}

// Start begins the cron scheduler. Checks every minute for pipelines with schedule triggers.
// Safe to call once; second call is a no-op.
func (s *CronScheduler) Start(ctx context.Context) {
	if s.started {
		return
	}
	s.started = true
	ctx, s.cancel = context.WithCancel(ctx)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.checkSchedules(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	s.logger.Info("cron scheduler started")
}

// Stop stops the cron scheduler and waits for it to finish.
func (s *CronScheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *CronScheduler) checkSchedules(ctx context.Context) {
	pipelines, err := s.pipelines.List(ctx)
	if err != nil {
		s.logger.Warn("cron: failed to list pipelines", zap.Error(err))
		return
	}

	now := time.Now()
	for _, p := range pipelines {
		for _, trigger := range p.Triggers {
			if trigger.Type != pipeline.TriggerCron {
				continue
			}

			cronExpr, _ := trigger.Config["cron"].(string)
			if cronExpr == "" {
				continue
			}

			if shouldRunCron(cronExpr, now) {
				// Skip if a run is already in progress for this pipeline
				runs, _ := s.pipelineSvc.ListRuns(ctx, p.ID)
				hasActive := false
				for _, r := range runs {
					if r.Status == pipeline.RunPending || r.Status == pipeline.RunRunning {
						hasActive = true
						break
					}
				}
				if hasActive {
					continue
				}

				s.logger.Info("cron: triggering pipeline",
					zap.String("pipeline", p.Name),
					zap.String("cron", cronExpr),
				)
				if _, err := s.pipelineSvc.Run(ctx, p.ID, fmt.Sprintf("cron(%s)", cronExpr)); err != nil {
					s.logger.Warn("cron: failed to run pipeline",
						zap.String("pipeline", p.Name),
						zap.Error(err),
					)
				}
			}
		}
	}
}

// shouldRunCron does a simple minute-level cron check.
// Supports basic cron format: minute hour day month weekday
// Uses * for wildcard. Does NOT support ranges, steps, or lists.
// shouldRunCron checks if the cron expression matches the current time.
// Supports standard 5-field cron: minute hour day month weekday.
// Field syntax: *, N, */N, N-M, N,M,O (and combinations).
func shouldRunCron(expr string, now time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}

	checks := []struct {
		field string
		value int
		max   int
	}{
		{fields[0], now.Minute(), 59},
		{fields[1], now.Hour(), 23},
		{fields[2], now.Day(), 31},
		{fields[3], int(now.Month()), 12},
		{fields[4], int(now.Weekday()), 7},
	}

	for _, c := range checks {
		if !matchCronField(c.field, c.value, c.max) {
			return false
		}
	}
	return true
}

// matchCronField checks if value matches a cron field expression.
// Supports: * (any), N (exact), */N (every N), N-M (range), N,M (list).
func matchCronField(field string, value, max int) bool {
	// Handle comma-separated list: "1,15,30"
	for _, part := range strings.Split(field, ",") {
		if matchCronPart(strings.TrimSpace(part), value, max) {
			return true
		}
	}
	return false
}

func matchCronPart(part string, value, max int) bool {
	// Wildcard: *
	if part == "*" {
		return true
	}

	// Step: */N or N-M/S
	if strings.Contains(part, "/") {
		pieces := strings.SplitN(part, "/", 2)
		step, err := strconv.Atoi(pieces[1])
		if err != nil || step <= 0 {
			return false
		}
		base := pieces[0]
		if base == "*" {
			return value%step == 0
		}
		// Range with step: N-M/S
		if strings.Contains(base, "-") {
			lo, hi, ok := parseRange(base)
			if !ok {
				return false
			}
			return value >= lo && value <= hi && (value-lo)%step == 0
		}
		return false
	}

	// Range: N-M
	if strings.Contains(part, "-") {
		lo, hi, ok := parseRange(part)
		if !ok {
			return false
		}
		return value >= lo && value <= hi
	}

	// Exact: N
	n, err := strconv.Atoi(part)
	if err != nil {
		return false
	}
	return n == value
}

func parseRange(s string) (lo, hi int, ok bool) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	lo, err1 := strconv.Atoi(parts[0])
	hi, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return lo, hi, true
}
