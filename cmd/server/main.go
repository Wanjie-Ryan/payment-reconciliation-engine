package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"

	ledgerhttp "github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/http"
	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/ledger"
	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/postgres"
	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/providerclient"
	"github.com/Wanjie-Ryan/payment-reconciliation-engine/internal/reconciliation"
)

func logsInit() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

func buildDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		os.Getenv("DATABASE_USER"),
		os.Getenv("DATABASE_PASSWORD"),
		os.Getenv("DATABASE_HOST"),
		os.Getenv("DATABASE_PORT"),
		os.Getenv("DATABASE_NAME"),
	)
}

func performMigration() {

	logrus.WithFields(logrus.Fields{"description": "starting migration"}).Info("performing Migration")

	db, err := sql.Open("pgx", buildDSN())

	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"description": "failed to open DB for migration"}).Fatal(err.Error())
	}

	defer db.Close()

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})

	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"description": "failed to init migration driver",
		}).Fatal(err.Error())
	}

	m, err := migrate.NewWithDatabaseInstance("file:///migrations", "pgx5", driver)

	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"description": "failed to init migrate instance"}).Fatal(err.Error())
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		logrus.WithError(err).WithFields(logrus.Fields{
			"description": "migration failed",
		}).Fatal(err.Error())
	}

	logrus.WithFields(logrus.Fields{"description": "migrations applied successfully"}).Info("perform migrations")

}

// connectDB gets a Postgres connection pool
func connectDB(ctx context.Context) (*pgxpool.Pool, error) {

	// creds to connect to the DB
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

	setupType := os.Getenv("SETUP_TYPE")
	if setupType == "" {
		setupType = "all"
	}

	if setupType == "cronjob" {
		performMigration()
		return
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

	providerClient := providerclient.New(os.Getenv("MOCK_PROVIDER_URL"))

	transactionRepo := postgres.NewTransactionRepository(pool)

	ledgerService := ledger.NewLedgerService(
		postgres.NewAccountRepository(pool),
		transactionRepo,
		providerClient,
	)
	ledgerhttp.RegisterRoutes(e, ledgerService)

	gracePeriod, err := time.ParseDuration(os.Getenv("RECONCILIATION_GRACE_PERIOD"))
	if err != nil {
		gracePeriod = 30 * time.Second
	}

	reconciliationService := reconciliation.NewService(
		postgres.NewStatementLineRepository(pool),
		postgres.NewDiscrepancyRepository(pool),
		transactionRepo,
		transactionRepo,
		gracePeriod,
	)
	ledgerhttp.RegisterWebhookRoutes(e, reconciliationService)

	replayService := ledger.NewReplayService(postgres.NewEventRepository(pool), postgres.NewAccountRepository(pool))
	ledgerhttp.RegisterAdminRoutes(e, replayService, reconciliationService)

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
