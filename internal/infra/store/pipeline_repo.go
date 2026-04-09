package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/erfianugrah/composer/internal/domain/pipeline"
)

// PipelineRepo implements pipeline.PipelineRepository using database/sql.
type PipelineRepo struct {
	db *sql.DB
}

func NewPipelineRepo(db *sql.DB) *PipelineRepo {
	return &PipelineRepo{db: db}
}

func (r *PipelineRepo) Create(ctx context.Context, p *pipeline.Pipeline) error {
	config, err := json.Marshal(pipelineConfig{Steps: p.Steps, Triggers: p.Triggers})
	if err != nil {
		return fmt.Errorf("marshaling pipeline config: %w", err)
	}
	_, err = r.db.ExecContext(ctx, //nolint:reassign
		`INSERT INTO pipelines (id, name, description, config, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		p.ID, p.Name, p.Description, string(config), p.CreatedBy, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (r *PipelineRepo) GetByID(ctx context.Context, id string) (*pipeline.Pipeline, error) {
	p := &pipeline.Pipeline{}
	var configStr string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, config, created_by, created_at, updated_at
		 FROM pipelines WHERE id = $1`, id,
	).Scan(&p.ID, &p.Name, &p.Description, &configStr, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg pipelineConfig
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	p.Steps = cfg.Steps
	p.Triggers = cfg.Triggers
	return p, nil
}

func (r *PipelineRepo) List(ctx context.Context) ([]*pipeline.Pipeline, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, config, created_by, created_at, updated_at
		 FROM pipelines ORDER BY name ASC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pipelines []*pipeline.Pipeline
	for rows.Next() {
		p := &pipeline.Pipeline{}
		var configStr string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &configStr, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		var cfg pipelineConfig
		if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
			return nil, fmt.Errorf("unmarshaling pipeline %s config: %w", p.ID, err)
		}
		p.Steps = cfg.Steps
		p.Triggers = cfg.Triggers
		pipelines = append(pipelines, p)
	}
	return pipelines, rows.Err()
}

func (r *PipelineRepo) Update(ctx context.Context, p *pipeline.Pipeline) error {
	config, err := json.Marshal(pipelineConfig{Steps: p.Steps, Triggers: p.Triggers})
	if err != nil {
		return fmt.Errorf("marshaling pipeline config: %w", err)
	}
	result, err := r.db.ExecContext(ctx,
		`UPDATE pipelines SET name=$2, description=$3, config=$4, updated_at=$5 WHERE id=$1`,
		p.ID, p.Name, p.Description, string(config), time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotUpdated
	}
	return nil
}

func (r *PipelineRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM pipelines WHERE id=$1`, id)
	return err
}

// RunRepo implements pipeline.RunRepository using database/sql.
type RunRepo struct {
	db *sql.DB
}

func NewRunRepo(db *sql.DB) *RunRepo {
	return &RunRepo{db: db}
}

func (r *RunRepo) Create(ctx context.Context, run *pipeline.Run) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO pipeline_runs (id, pipeline_id, status, triggered_by, started_at, finished_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		run.ID, run.PipelineID, string(run.Status), run.TriggeredBy, run.StartedAt, run.FinishedAt, run.CreatedAt,
	)
	return err
}

func (r *RunRepo) GetByID(ctx context.Context, id string) (*pipeline.Run, error) {
	run := &pipeline.Run{}
	var status string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, pipeline_id, status, triggered_by, started_at, finished_at, created_at
		 FROM pipeline_runs WHERE id = $1`, id,
	).Scan(&run.ID, &run.PipelineID, &status, &run.TriggeredBy, &run.StartedAt, &run.FinishedAt, &run.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run.Status = pipeline.RunStatus(status)
	return run, nil
}

func (r *RunRepo) ListByPipeline(ctx context.Context, pipelineID string) ([]*pipeline.Run, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, pipeline_id, status, triggered_by, started_at, finished_at, created_at
		 FROM pipeline_runs WHERE pipeline_id = $1 ORDER BY created_at DESC LIMIT 50`, pipelineID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*pipeline.Run
	for rows.Next() {
		run := &pipeline.Run{}
		var status string
		if err := rows.Scan(&run.ID, &run.PipelineID, &status, &run.TriggeredBy, &run.StartedAt, &run.FinishedAt, &run.CreatedAt); err != nil {
			return nil, err
		}
		run.Status = pipeline.RunStatus(status)
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (r *RunRepo) Update(ctx context.Context, run *pipeline.Run) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE pipeline_runs SET status=$2, started_at=$3, finished_at=$4 WHERE id=$1`,
		run.ID, string(run.Status), run.StartedAt, run.FinishedAt,
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotUpdated
	}
	return nil
}

// pipelineConfig is the JSON structure stored in the pipelines.config column.
type pipelineConfig struct {
	Steps    []pipeline.Step    `json:"steps"`
	Triggers []pipeline.Trigger `json:"triggers"`
}
