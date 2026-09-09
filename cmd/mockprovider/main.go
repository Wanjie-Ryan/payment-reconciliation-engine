package main

import (
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

func logsInit() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
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

	e := echo.New()
	e.HideBanner = true
	e.Use(loggingMiddleware)

	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "service": "mock-payment-provider"})
	})

	port := os.Getenv("MOCK_PROVIDER_PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port

	logrus.WithFields(logrus.Fields{
		"description": "starting mock payment provider",
		"addr":        addr,
	}).Info("main")

	if err := e.Start(addr); err != nil {

		logrus.WithError(err).WithFields(logrus.Fields{
			"description": "server stopped",
		}).Error(err.Error())
	}
}
