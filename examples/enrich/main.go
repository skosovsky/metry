package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func run() int {
	ctx := context.Background()
	mem := testutil.NewInMemoryTraceExporter()
	provider, err := metry.New(ctx,
		metry.WithServiceName("enrich-example"),
		metry.WithExporter(metrytest.MetrySpanExporter(mem)),
	)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer func() { _ = provider.Shutdown(ctx) }()

	var buf bytes.Buffer
	logger := slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)})

	ctx, end, err := provider.StartSpan(ctx, "example", "request")
	if err != nil {
		log.Println(err)
		return 1
	}
	defer end()
	const exampleScore = 0.91
	ctx = metry.Enrich(ctx, metry.TenantID("tenant-1"), metry.PatientID("patient-9"),
		metry.FloatAttribute("score", exampleScore), metry.BoolAttribute("passed", true))
	logger.InfoContext(ctx, "enriched request")

	fmt.Printf("sample log line: %s\n", json.RawMessage(buf.Bytes()))
	return 0
}

func main() {
	os.Exit(run())
}
