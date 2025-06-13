package actions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kaito-project/kaito/test/suite/clients/ragengine"
	"github.com/kaito-project/kaito/test/suite/types"
)

// CreateIndex creates an index in the RAG engine and indexes the provided documents.
// It also saves the indexed documents to the provided slice for later verification.
func CreateIndex(indexName string, documents []*ragengine.RAGDocument, savedDocuments []*ragengine.RAGDocument) types.Action {
	return types.Action{
		Name: "Create Index",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.RAGEngineTestContext) error {
			req := &ragengine.IndexDocumentRequest{
				IndexName: indexName,
				Documents: documents,
			}
			docs, err := testContext.RAGClient.IndexDocuments(req)
			if err != nil {
				return fmt.Errorf("failed to index documents: %v", err)
			}

			for idx, doc := range docs {
				if doc.Text != req.Documents[idx].Text {
					return fmt.Errorf("indexed document text mismatch: expected %s, got %s", req.Documents[idx].Text, doc.Text)
				}
				for k, v := range req.Documents[idx].Metadata {
					if doc.Metadata[k] != v {
						return fmt.Errorf("indexed document metadata mismatch for key %s: expected %v, got %v", k, v, doc.Metadata[k])
					}
				}
				logger.Info(fmt.Sprintf("Document %d indexed successfully with ID: %s\n", idx, doc.DocID))
			}

			savedDocuments = append(savedDocuments, docs...)
			return nil
		},
		CleanupFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.RAGEngineTestContext) error {
			indexes, err := testContext.RAGClient.ListIndexes()
			if err != nil {
				return fmt.Errorf("failed to list indexes after creating index %s: %v", indexName, err)
			}
			found := false
			for _, index := range indexes {
				if index == indexName {
					found = true
					break
				}
			}

			// If the index was not found, it means the index was already deleted or never created
			if !found {
				return nil
			}

			err = testContext.RAGClient.DeleteIndex(indexName)
			if err != nil {
				return fmt.Errorf("failed to delete index %s after test: %v", indexName, err)
			}

			return nil
		},
	}
}

// QueryIndex queries the specified index with the given query string.
// It checks if the response contains any source nodes and returns an error if not.
func QueryIndex(indexName, query string, topK, maxTokens int, temperature float64) types.Action {
	return types.Action{
		Name: "Query Index",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.RAGEngineTestContext) error {
			req := &ragengine.QueryRequest{
				IndexName: indexName,
				Query:     query,
				TopK:      topK,
				LLMParams: ragengine.QueryLLMParams{
					Temperature: temperature,
					MaxTokens:   maxTokens,
				},
			}

			resp, err := testContext.RAGClient.Query(req)
			if err != nil {
				return fmt.Errorf("failed to query index %s: %v", indexName, err)
			}

			if len(resp.SourceNodes) == 0 {
				return fmt.Errorf("no results found for query: %s", query)
			}

			return nil
		},
	}
}

// ListIndexes lists all indexes in the RAG engine and checks if the expected indexes are present.
func ListIndexes(expectedIndexes []string) types.Action {
	return types.Action{
		Name: "List Indexes",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.RAGEngineTestContext) error {
			indexes, err := testContext.RAGClient.ListIndexes()
			if err != nil {
				return fmt.Errorf("failed to list indexes: %v", err)
			}

			for _, expectedIndex := range expectedIndexes {
				found := false
				for _, index := range indexes {
					if index == expectedIndex {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("expected index %s not found in the list of indexes from rag", expectedIndex)
				}
			}

			return nil
		},
	}
}

// GetAllIndexDocuments retrieves all documents in the specified index and checks if the expected documents are present.
func GetAllIndexDocuments(indexName string, expectedDocuments []*ragengine.RAGDocument) types.Action {
	return types.Action{
		Name: "Get All Documents in Index",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.RAGEngineTestContext) error {

			allDocs := []*ragengine.RAGDocument{}
			offset := 0
			for {
				resp, err := testContext.RAGClient.ListDocumentsInIndex(indexName, 100, offset, 1000, nil)
				if err != nil {
					return fmt.Errorf("failed to list documents in index %s: %v", indexName, err)
				}

				allDocs = append(allDocs, resp.Documents...)

				if resp.Count < 100 {
					break
				}
				offset += 100
			}

			for _, doc := range expectedDocuments {
				found := false
				for _, listedDoc := range allDocs {
					if listedDoc.DocID == doc.DocID {
						found = true
						if !listedDoc.IsTruncated && listedDoc.Text != doc.Text {
							return fmt.Errorf("document text mismatch for DocID %s: expected %s, got %s", doc.DocID, doc.Text, listedDoc.Text)
						}
						for k, v := range doc.Metadata {
							if listedDoc.Metadata[k] != v {
								return fmt.Errorf("document metadata mismatch for DocID %s, key %s: expected %v, got %v", doc.DocID, k, v, listedDoc.Metadata[k])
							}
						}
						break
					}
				}
				if !found {
					return fmt.Errorf("expected document with DocID %s not found in the listed documents", doc.DocID)
				}
			}
			return nil
		},
	}
}

// DeleteIndex deletes the specified index from the RAG engine.
func DeleteIndex(indexName string) types.Action {
	return types.Action{
		Name: "Delete Index",
		RunFunc: func(ctx context.Context, logger *slog.Logger, testContext *types.RAGEngineTestContext) error {
			err := testContext.RAGClient.DeleteIndex(indexName)
			if err != nil {
				logger.Error(fmt.Sprintf("failed to delete index %s: %v", indexName, err))
				return fmt.Errorf("failed to delete index %s: %v", indexName, err)
			}
			return nil
		},
	}
}
