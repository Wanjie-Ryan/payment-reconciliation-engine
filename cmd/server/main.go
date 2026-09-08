package server

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)


func logsInit(){
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

// connectDB gets a Postgres connection pool
func connectDB(ctx context.Context) (*pgxpool.Pool, error){

	host := os.Getenv("DATABASE_HOST")
	port := os.Getenv("DATABASE_PORT")
	username := os.Getenv("DATABASE_USER")
	password := os.Getenv("DATABASE_PASSWORD")
	dbname := os.Getenv("DATABASE_NAME")

	logrus.WithContext(ctx).WithFields(logrus.Fields{
		"description":"connecting to DB",
		"host": host,
		"port": port,
		"databaseName":dbname,
	}).Info("connectDB")

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", username, password, host, port, dbname)


}