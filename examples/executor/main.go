package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/middleware/executor"
)

type request struct {
	ID string
}

type response struct {
	OK bool
}

// Executor is the minimal shape many routers and agent runtimes use; metry does not import them.
type Executor[Req, Res any] interface {
	Execute(ctx context.Context, req Req) (Res, error)
}

// ExecutorFunc adapts a function to Executor without a third-party dependency.
type ExecutorFunc[Req, Res any] func(context.Context, Req) (Res, error)

func (f ExecutorFunc[Req, Res]) Execute(ctx context.Context, req Req) (Res, error) {
	return f(ctx, req)
}

func run() int {
	ctx := context.Background()
	provider, err := metry.New(ctx, metry.WithServiceName("executor-example"))
	if err != nil {
		log.Println(err)
		return 1
	}
	defer func() { _ = provider.Shutdown(ctx) }()

	core := func(_ context.Context, req request) (response, error) {
		if req.ID == "" {
			return response{}, errors.New("empty id")
		}
		return response{OK: true}, nil
	}

	wrapped := executor.Wrap(provider, "example.operation", core)
	var exec Executor[request, response] = ExecutorFunc[request, response](wrapped)

	out, err := exec.Execute(ctx, request{ID: "1"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("ok=%v\n", out.OK)
	return 0
}

func main() {
	os.Exit(run())
}
