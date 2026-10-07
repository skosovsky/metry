package main

import (
	"context"
	"fmt"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
)

const terminalDeltaTokens = 3

func run(ctx context.Context) error {
	provider, err := metry.New(ctx, metry.WithServiceName("mapping-example"))
	if err != nil {
		return err
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()
	tracker, err := genai.NewTrackerFromProvider(provider, genai.WithPayloadPolicy(genai.RedactPayloadPolicy()))
	if err != nil {
		return err
	}
	if ownerErr := checkOwner([]string{"model-attempt"}); ownerErr != nil {
		return ownerErr
	}
	_, handle, err := tracker.Recorder().StartOperation(ctx,
		genai.Operation{Name: "chat", Locality: genai.ExecutionRemote}, genai.RecordingOptions{})
	if err != nil {
		return err
	}
	// Returning the handler would not finish this stream. Only its terminal result does.
	usage, err := normalizeUpdates(
		[]usageUpdate{
			{sequence: 1, mode: deltaMode, tokens: 2},
			{sequence: 2, mode: deltaMode, tokens: terminalDeltaTokens, terminal: true},
		},
	)
	if err != nil {
		return err
	}
	mapped, mapErr := mapModel(modelResult{state: "completed", usage: usage})
	if mapErr != nil {
		return mapErr
	}
	if finishErr := handle.Finish(mapped.result, mapped.attributes...); finishErr != nil {
		return finishErr
	}

	evaluation, err := mapAssessment(
		readyAssessment{id: "case/assertion/trial", metric: "answer_relevance", state: "assertion", assertion: false},
		nil,
	)
	if err != nil {
		return err
	}
	if err := tracker.EvaluationRecorder().Record(ctx, evaluation); err != nil {
		return err
	}
	var sink measurementSink
	// Acknowledgement loss does not imply a failed commit; replay has the same identity.
	_ = sink.accept(evaluation, true)
	return sink.accept(evaluation, false)
}

func main() {
	if err := run(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println("mapped terminal usage and ready assessment; host replay accepted")
}
