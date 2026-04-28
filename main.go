package main

import (
	"context"
	"log"
	"os"

	"github.com.patrhez/database-tx-pattern/task"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		log.Println("set MYSQL_DSN to run the example")
		return
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	repo := task.NewRepository(db)
	txManager := task.NewGormTxManager(db)
	service := task.NewTaskService(repo, txManager)

	if err := service.RenameTaskWithLock(context.Background(), 7, "new-name"); err != nil {
		log.Fatalf("rename task with lock: %v", err)
	}

	log.Println("transactional call completed")
}
