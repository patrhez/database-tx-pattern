package task

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Task maps to the tasks table in MySQL.
type Task struct {
	ID        uint      `gorm:"column:id;primaryKey;autoIncrement"`
	Name      string    `gorm:"column:name;type:varchar(255);not null"`
	Type      string    `gorm:"column:type;type:varchar(100);not null"`
	Creator   string    `gorm:"column:creator;type:varchar(100);not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (Task) TableName() string {
	return "tasks"
}

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, t *Task) error {
	if t == nil {
		return errors.New("task is nil")
	}
	return r.dbFor(ctx).WithContext(ctx).Create(t).Error
}

func (r *Repository) GetByID(ctx context.Context, id uint) (*Task, error) {
	var t Task
	err := r.dbFor(ctx).WithContext(ctx).First(&t, id).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repository) GetByIDForUpdate(ctx context.Context, id uint) (*Task, error) {
	var t Task
	err := r.dbFor(ctx).WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&t, id).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repository) UpdateName(ctx context.Context, id uint, name string) error {
	if name == "" {
		return errors.New("name cannot be empty")
	}
	return r.dbFor(ctx).WithContext(ctx).
		Model(&Task{}).
		Where("id = ?", id).
		Update("name", name).Error
}

func (r *Repository) dbFor(ctx context.Context) *gorm.DB {
	return dbFromContext(ctx, r.db)
}
