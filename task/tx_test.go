package task

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestTaskService_RenameTaskWithLock_UsesRealGormTxManager(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "name", "type", "creator", "created_at", "updated_at"}).
		AddRow(7, "old-name", "engineering", "alice", now, now)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `tasks` WHERE `tasks`.`id` = ? ORDER BY `tasks`.`id` LIMIT ? FOR UPDATE")).
		WithArgs(7, 1).
		WillReturnRows(rows)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `tasks` SET `name`=?,`updated_at`=? WHERE id = ?")).
		WithArgs("new-name", sqlmock.AnyArg(), 7).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := NewRepository(db)
	tx := NewGormTxManager(db)
	svc := NewTaskService(repo, tx)

	err := svc.RenameTaskWithLock(context.Background(), 7, "new-name")
	require.NoError(t, err)
}
