package task

import (
	"context"
	"errors"
)

// TaskStore is the repository contract used by the service layer.
// In tests, this can be replaced with an in-memory fake.
type TaskStore interface {
	Create(ctx context.Context, t *Task) error
	GetByID(ctx context.Context, id uint) (*Task, error)
	GetByIDForUpdate(ctx context.Context, id uint) (*Task, error)
	UpdateName(ctx context.Context, id uint, name string) error
}

// TxManager abstracts transaction control away from the service.
type TxManager interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type TaskService struct {
	store TaskStore
	tx    TxManager
}

func NewTaskService(store TaskStore, tx TxManager) *TaskService {
	return &TaskService{
		store: store,
		tx:    tx,
	}
}

func (s *TaskService) CreateTask(ctx context.Context, name, taskType, creator string) (*Task, error) {
	if name == "" || taskType == "" || creator == "" {
		return nil, errors.New("name, type and creator are required")
	}

	t := &Task{
		Name:    name,
		Type:    taskType,
		Creator: creator,
	}

	if err := s.store.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *TaskService) GetTask(ctx context.Context, id uint) (*Task, error) {
	return s.store.GetByID(ctx, id)
}

// RenameTaskWithLock demonstrates a common transactional flow:
// 1) read row with lock (SELECT ... FOR UPDATE),
// 2) apply business change,
// 3) persist update in same transaction.
func (s *TaskService) RenameTaskWithLock(ctx context.Context, id uint, newName string) error {
	if newName == "" {
		return errors.New("new name cannot be empty")
	}

	return s.tx.InTx(ctx, func(txCtx context.Context) error {
		existing, err := s.store.GetByIDForUpdate(txCtx, id)
		if err != nil {
			return err
		}
		if existing == nil {
			return errors.New("task not found")
		}
		return s.store.UpdateName(txCtx, id, newName)
	})
}

