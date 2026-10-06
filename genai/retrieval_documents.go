package genai

import (
	"encoding/json"
	"math"
	"unicode/utf8"
)

// The selected upstream document schema permits these explicitly declared extensions.
type retrievalDocumentWire struct {
	ID      string   `json:"id,omitempty"`
	Version string   `json:"metry.document.version,omitempty"`
	Digest  string   `json:"metry.document.digest,omitempty"`
	Score   *float64 `json:"score,omitempty"`
}

// prepareRetrievalDocuments validates the entire serialized input before policy or writes.
func prepareRetrievalDocuments(result RetrievalResult) ([]retrievalDocumentWire, error) {
	if result.ReturnedDocuments < 0 || len(result.Documents) > maxPayloadEntries {
		return nil, invalidObservation("retrieval_result", "input_bounds")
	}
	documents := make([]retrievalDocumentWire, len(result.Documents))
	for index, document := range result.Documents {
		if !boundedRetrievalReference(document.ID) || !boundedRetrievalReference(document.Version) ||
			!boundedRetrievalReference(document.Digest) {
			return nil, invalidObservation("retrieval_document", "input_bounds")
		}
		wire := retrievalDocumentWire{ID: document.ID, Version: document.Version, Digest: document.Digest, Score: nil}
		if document.Relevance != nil {
			score := *document.Relevance
			if math.IsNaN(score) || math.IsInf(score, 0) {
				return nil, invalidObservation("retrieval_relevance", "non_finite")
			}
			wire.Score = &score
		}
		documents[index] = wire
	}
	data, err := json.Marshal(documents)
	if err != nil {
		return nil, invalidObservation("retrieval_document", "invalid_encoding")
	}
	if _, err = decodePayloadJSON(string(data)); err != nil {
		return nil, err
	}
	return documents, nil
}

func protectRetrievalDocuments(documents []retrievalDocumentWire, cfg recorderConfig) {
	for index := range documents {
		document := &documents[index]
		document.ID = referenceWithConfig(document.ID, cfg)
		document.Version = referenceWithConfig(document.Version, cfg)
		document.Digest = referenceWithConfig(document.Digest, cfg)
	}
}

func boundedRetrievalReference(value string) bool {
	return len(value) <= maxIdentityBytes && utf8.ValidString(value)
}
