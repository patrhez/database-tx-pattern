package task

import (
	"context"

	"gorm.io/gorm"
)

type txContextKey struct{}

// GormTxManager is the production transaction manager backed by gorm.DB.
// It starts a transaction and injects the transactional handle into the context
// so repository methods can join it without changing their method signatures.
type GormTxManager struct {
	db *gorm.DB
}

func NewGormTxManager(db *gorm.DB) *GormTxManager {
	return &GormTxManager{db: db}
}

func (m *GormTxManager) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(withTx(ctx, tx))
	})
}

func withTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

func dbFromContext(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	tx, ok := ctx.Value(txContextKey{}).(*gorm.DB)
	if !ok || tx == nil {
		return fallback
	}
	return tx
}
