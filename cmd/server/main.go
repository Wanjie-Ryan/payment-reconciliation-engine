package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

func logsInit() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

// connectDB gets a Postgres connection pool
func connectDB(ctx context.Context) (*pgxpool.Pool, error) {

	host := os.Getenv("DATABASE_HOST")
	port := os.Getenv("DATABASE_PORT")
	username := os.Getenv("DATABASE_USER")
	password := os.Getenv("DATABASE_PASSWORD")
	dbname := os.Getenv("DATABASE_NAME")

	logrus.WithContext(ctx).WithFields(logrus.Fields{
		"description":  "connecting to DB",
		"host":         host,
		"port":         port,
		"databaseName": dbname,
	}).Info("connectDB")

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", username, password, host, port, dbname)

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(connectCtx, dsn)

	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{
			"description": "error creating DB connection pool",
		}).Error(err.Error())

		return nil, err

	}

	if err = pool.Ping(connectCtx); err != nil {
		pool.Close()
		logrus.WithContext(ctx).WithFields(logrus.Fields{
			"description": "error pinging DB",
		}).Error(err.Error())

		return nil, err
	}

	return pool, nil

}

func loggingMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		start := time.Now()
		err := next(c)

		logrus.WithContext(c.Request().Context()).WithFields(logrus.Fields{
			"description": "request handled",
			"method":      c.Request().Method,
			"path":        c.Path(),
			"status":      c.Response().Status,
			"duration_ms": time.Since(start).Milliseconds(),
		}).Info("request handled")
		return err
	}
}

func main() {
	logsInit()

	if err := godotenv.Load(); err != nil {

		logrus.WithFields(logrus.Fields{
			"description": "no .env file found, relying on process environment",
		}).Info("godotenv")
	}

	ctx := context.Background()

	pool, err := connectDB(ctx)
	if err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{
			"description": "failed to connect to DB",
		}).Fatal(err.Error())
	}
	defer pool.Close()

	e := echo.New()
	e.HideBanner = true
	e.Use(loggingMiddleware)

	e.GET("/health", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{
				"description": "health check failed",
			}).Error(err.Error())

			return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "down", "error": err.Error()})
		}

		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})

	})

	appPort := os.Getenv("APP_PORT")
	if appPort == "" {
		appPort = "8080"
	}

	addr := ":" + appPort

	logrus.WithFields(logrus.Fields{
		"description": "starting ledger service",
		"addr":        addr,
	}).Info("Starting ledger service at port 8080")

	if err := e.Start(addr); err != nil {
		logrus.WithContext(ctx).WithFields(logrus.Fields{
			"description": "failed to start server",
		}).Error(err.Error())
	}

}
