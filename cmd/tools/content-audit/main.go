package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/services/contentaudit"
	"github.com/alemancenter/fiber-api/pkg/logger"
)

const defaultOutputPath = "policy_audit_report.csv"

func main() {
	outputPath := flag.String("output", defaultOutputPath, "CSV report output path")
	flag.Parse()

	cfg := config.Load()
	cfg.App.Debug = false
	log := logger.Init(logger.Config{Level: "warn", Debug: false})
	defer log.Sync()

	findings, err := contentaudit.Scan(context.Background(), contentaudit.Options{Config: cfg})
	if err != nil {
		exitf("content audit failed: %v", err)
	}

	file, err := os.Create(*outputPath)
	if err != nil {
		exitf("failed to create %s: %v", *outputPath, err)
	}
	defer file.Close()

	if err := contentaudit.WriteCSV(file, findings); err != nil {
		exitf("failed to write CSV report: %v", err)
	}

	fmt.Printf("content audit completed: %s (%d findings)\n", *outputPath, len(findings))
}

func exitf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
