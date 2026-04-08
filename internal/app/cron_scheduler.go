package app

import (
	"context"
	"fmt"
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
}

func NewCronScheduler(pipelineSvc *PipelineService, pipelines pipeline.PipelineRepository, logger *zap.Logger) *CronScheduler {
	return &CronScheduler{
		pipelineSvc: pipelineSvc,
		pipelines:   pipelines,
		logger:      logger,
	}
}

// Start begins the cron scheduler. Checks every minute for pipelines with schedule triggers.
func (s *CronScheduler) Start(ctx context.Context) {
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
func shouldRunCron(expr string, now time.Time) bool {
	fields := splitFields(expr)
	if len(fields) != 5 {
		return false
	}

	checks := []struct {
		field string
		value int
	}{
		{fields[0], now.Minute()},
		{fields[1], now.Hour()},
		{fields[2], now.Day()},
		{fields[3], int(now.Month())},
		{fields[4], int(now.Weekday())},
	}

	for _, c := range checks {
		if c.field == "*" {
			continue
		}
		var val int
		if _, err := fmt.Sscanf(c.field, "%d", &val); err != nil {
			return false
		}
		if val != c.value {
			return false
		}
	}

	return true
}

func splitFields(s string) []string {
	var fields []string
	current := ""
	for _, c := range s {
		if c == ' ' || c == '\t' {
			if current != "" {
				fields = append(fields, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		fields = append(fields, current)
	}
	return fields
}
