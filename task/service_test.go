package task

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	createFn         func(ctx context.Context, t *Task) error
	getByIDFn        func(ctx context.Context, id uint) (*Task, error)
	getByIDForUpdate func(ctx context.Context, id uint) (*Task, error)
	updateNameFn     func(ctx context.Context, id uint, name string) error
}

func (f *fakeStore) Create(ctx context.Context, t *Task) error {
	return f.createFn(ctx, t)
}

func (f *fakeStore) GetByID(ctx context.Context, id uint) (*Task, error) {
	return f.getByIDFn(ctx, id)
}

func (f *fakeStore) GetByIDForUpdate(ctx context.Context, id uint) (*Task, error) {
	return f.getByIDForUpdate(ctx, id)
}

func (f *fakeStore) UpdateName(ctx context.Context, id uint, name string) error {
	return f.updateNameFn(ctx, id, name)
}

type fakeTxManager struct {
	inTxFn func(ctx context.Context, fn func(ctx context.Context) error) error
}

func (f *fakeTxManager) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return f.inTxFn(ctx, fn)
}

func TestTaskService_CreateTask(t *testing.T) {
	called := false
	store := &fakeStore{
		createFn: func(ctx context.Context, task *Task) error {
			called = true
			task.ID = 100
			return nil
		},
		getByIDFn:        func(ctx context.Context, id uint) (*Task, error) { return nil, nil },
		getByIDForUpdate: func(ctx context.Context, id uint) (*Task, error) { return nil, nil },
		updateNameFn:     func(ctx context.Context, id uint, name string) error { return nil },
	}
	svc := NewTaskService(store, &fakeTxManager{})

	task, err := svc.CreateTask(context.Background(), "write docs", "engineering", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected repository Create to be called")
	}
	if task.ID != 100 {
		t.Fatalf("expected created task id 100, got %d", task.ID)
	}
}

func TestTaskService_RenameTaskWithLock_Success(t *testing.T) {
	var callOrder []string

	store := &fakeStore{
		createFn:  func(ctx context.Context, t *Task) error { return nil },
		getByIDFn: func(ctx context.Context, id uint) (*Task, error) { return nil, nil },
		getByIDForUpdate: func(ctx context.Context, id uint) (*Task, error) {
			callOrder = append(callOrder, "lock-read")
			return &Task{ID: id, Name: "old-name"}, nil
		},
		updateNameFn: func(ctx context.Context, id uint, name string) error {
			callOrder = append(callOrder, "update")
			if name != "new-name" {
				return errors.New("unexpected name")
			}
			return nil
		},
	}

	tx := &fakeTxManager{
		inTxFn: func(ctx context.Context, fn func(ctx context.Context) error) error {
			callOrder = append(callOrder, "begin-tx")
			err := fn(ctx)
			if err != nil {
				callOrder = append(callOrder, "rollback")
				return err
			}
			callOrder = append(callOrder, "commit")
			return nil
		},
	}

	svc := NewTaskService(store, tx)
	err := svc.RenameTaskWithLock(context.Background(), 7, "new-name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := join(callOrder)
	want := "begin-tx,lock-read,update,commit"
	if got != want {
		t.Fatalf("unexpected call order, want %q, got %q", want, got)
	}
}

func TestTaskService_RenameTaskWithLock_LockReadError(t *testing.T) {
	store := &fakeStore{
		createFn:  func(ctx context.Context, t *Task) error { return nil },
		getByIDFn: func(ctx context.Context, id uint) (*Task, error) { return nil, nil },
		getByIDForUpdate: func(ctx context.Context, id uint) (*Task, error) {
			return nil, errors.New("db timeout")
		},
		updateNameFn: func(ctx context.Context, id uint, name string) error {
			t.Fatal("update should not be called when lock-read fails")
			return nil
		},
	}

	tx := &fakeTxManager{
		inTxFn: func(ctx context.Context, fn func(ctx context.Context) error) error {
			return fn(ctx)
		},
	}

	svc := NewTaskService(store, tx)
	err := svc.RenameTaskWithLock(context.Background(), 7, "new-name")
	if err == nil || err.Error() != "db timeout" {
		t.Fatalf("expected db timeout error, got %v", err)
	}
}

func TestTaskService_RenameTaskWithLock_EmptyName(t *testing.T) {
	store := &fakeStore{
		createFn:         func(ctx context.Context, t *Task) error { return nil },
		getByIDFn:        func(ctx context.Context, id uint) (*Task, error) { return nil, nil },
		getByIDForUpdate: func(ctx context.Context, id uint) (*Task, error) { return nil, nil },
		updateNameFn:     func(ctx context.Context, id uint, name string) error { return nil },
	}
	tx := &fakeTxManager{}
	svc := NewTaskService(store, tx)

	err := svc.RenameTaskWithLock(context.Background(), 7, "")
	if err == nil || err.Error() != "new name cannot be empty" {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func join(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for i := 1; i < len(items); i++ {
		out += "," + items[i]
	}
	return out
}

