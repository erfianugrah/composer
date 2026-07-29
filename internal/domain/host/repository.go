package host

import "context"

// Repository stores and retrieves Host aggregates.
type Repository interface {
	Create(ctx context.Context, h *Host) error
	GetByID(ctx context.Context, id int64) (*Host, error)
	GetByName(ctx context.Context, name string) (*Host, error)
	List(ctx context.Context) ([]*Host, error)
	Update(ctx context.Context, h *Host) error
	Delete(ctx context.Context, id int64) error
	// CountStacks returns how many stacks reference this host (delete guard).
	CountStacks(ctx context.Context, id int64) (int, error)
}
