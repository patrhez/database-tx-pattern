package task

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func setupMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	t.Log("setting up sqlmock connection and gorm(mysql) adapter")

	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	require.NoError(t, err)

	cleanup := func() {
		t.Log("verifying sqlmock expectations and closing mock sql db")
		mock.ExpectClose()
		t.Log("expect: close mock sql db")
		require.NoError(t, sqlDB.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}

	return gormDB, mock, cleanup
}

func TestRepository_Create(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()
	t.Log("scenario: create a task using INSERT into tasks table")

	now := time.Now()
	task := &Task{
		Name:      "write unit tests",
		Type:      "engineering",
		Creator:   "alice",
		CreatedAt: now,
		UpdatedAt: now,
	}

	mock.ExpectBegin()
	t.Log("expect: begin transaction")
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `tasks` (`name`,`type`,`creator`,`created_at`,`updated_at`) VALUES (?,?,?,?,?)")).
		WithArgs(task.Name, task.Type, task.Creator, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	t.Log("expect: insert SQL with task fields and auto-generated timestamps")
	mock.ExpectCommit()
	t.Log("expect: commit transaction")

	repo := NewRepository(db)
	err := repo.Create(context.Background(), task)
	t.Log("action: execute repository.Create")

	require.NoError(t, err)
	require.Equal(t, uint(1), task.ID)
	t.Logf("assert: create succeeded and task id = %d", task.ID)
}

func TestRepository_GetByID(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()
	t.Log("scenario: fetch task by id using SELECT")

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "name", "type", "creator", "created_at", "updated_at"}).
		AddRow(7, "refactor service", "engineering", "bob", now, now)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `tasks` WHERE `tasks`.`id` = ? ORDER BY `tasks`.`id` LIMIT ?")).
		WithArgs(7, 1).
		WillReturnRows(rows)
	t.Log("expect: select SQL by primary key and return one row")

	repo := NewRepository(db)
	task, err := repo.GetByID(context.Background(), 7)
	t.Log("action: execute repository.GetByID")

	require.NoError(t, err)
	require.Equal(t, uint(7), task.ID)
	require.Equal(t, "refactor service", task.Name)
	require.Equal(t, "engineering", task.Type)
	require.Equal(t, "bob", task.Creator)
	t.Logf("assert: selected task id=%d name=%q creator=%q", task.ID, task.Name, task.Creator)
}

func TestRepository_UpdateName(t *testing.T) {
	db, mock, cleanup := setupMockDB(t)
	defer cleanup()
	t.Log("scenario: update task name by id")

	mock.ExpectBegin()
	t.Log("expect: begin transaction")
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `tasks` SET `name`=?,`updated_at`=? WHERE id = ?")).
		WithArgs("new task name", sqlmock.AnyArg(), 10).
		WillReturnResult(sqlmock.NewResult(0, 1))
	t.Log("expect: update SQL to change name and updated_at for id=10")
	mock.ExpectCommit()
	t.Log("expect: commit transaction")

	repo := NewRepository(db)
	err := repo.UpdateName(context.Background(), 10, "new task name")
	t.Log("action: execute repository.UpdateName")

	require.NoError(t, err)
	t.Log("assert: update succeeded")
}

func TestRepository_UpdateName_EmptyName(t *testing.T) {
	db, _, cleanup := setupMockDB(t)
	defer cleanup()
	t.Log("scenario: reject empty task name before SQL execution")

	repo := NewRepository(db)
	err := repo.UpdateName(context.Background(), 10, "")
	t.Log("action: execute repository.UpdateName with empty name")

	require.EqualError(t, err, "name cannot be empty")
	t.Log("assert: business validation error returned")
}
