package actions

import (
	"context"
	"fmt"

	"github.com/kaito-project/kaito/test/suite/clients/ragengine"
	"github.com/kaito-project/kaito/test/suite/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func GetRagClient(serviceName, namespace string) types.Action {
	return types.Action{
		Name: "Get RAG Client",
		RunFunc: func(ctx context.Context, testContext *types.RAGEngineTestContext) error {
			if testContext.RAGClient != nil {
				fmt.Println("RAG client already initialized, skipping re-initialization")
				return nil
			}

			gvr := schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "services",
			}

			service, err := testContext.Cluster.DynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, serviceName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get RAGEngine resources: %v", err)
			}

			clusterIP, found, err := unstructured.NestedString(service.Object, "spec", "clusterIP")
			if err != nil || !found {
				return fmt.Errorf("failed to get clusterIP from service %s: %v", serviceName, err)
			}

			testContext.RAGClient = ragengine.NewClient(ragengine.RAGEngineConfig{
				URL: fmt.Sprintf("http://%s", clusterIP),
			})

			return nil
		},
	}
}

func CreateIndex(indexName string, documents []*ragengine.RAGDocument) types.Action {
	return types.Action{
		Name: "Create Index",
		RunFunc: func(ctx context.Context, testContext *types.RAGEngineTestContext) error {
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
				fmt.Printf("Document %d indexed successfully with ID: %s\n", idx, doc.DocID)
			}

			// Store the indexed documents in the test context for later use
			documents = docs
			return nil
		},
	}
}

func QueryIndex(indexName, query string, topK, maxTokens int, temperature float64) types.Action {
	return types.Action{
		Name: "Query Index",
		RunFunc: func(ctx context.Context, testContext *types.RAGEngineTestContext) error {
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

			fmt.Printf("Query response: %s\n", resp.Response)
			for _, node := range resp.SourceNodes {
				fmt.Printf("Source Node - DocID: %s, Score: %.4f, Text: %s\n", node.DocID, node.Score, node.Text)
			}
			return nil
		},
	}
}

func ListIndexes(expectedIndexes []string) types.Action {
	return types.Action{
		Name: "List Indexes",
		RunFunc: func(ctx context.Context, testContext *types.RAGEngineTestContext) error {
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

func ListDocumentsInIndex(indexName string, limit, offset, maxTextLength int, metadataFilter map[string]interface{}, expectedDocuments []*ragengine.RAGDocument) types.Action {
	return types.Action{
		Name: "List Documents in Index",
		RunFunc: func(ctx context.Context, testContext *types.RAGEngineTestContext) error {
			resp, err := testContext.RAGClient.ListDocumentsInIndex(indexName, limit, offset, maxTextLength, metadataFilter)
			if err != nil {
				return fmt.Errorf("failed to list documents in index %s: %v", indexName, err)
			}

			if resp.Count == 0 && len(expectedDocuments) > 0 {
				return fmt.Errorf("no documents found in index %s", indexName)
			}

			fmt.Printf("Found %d documents in index %s:\n", resp.Count, indexName)
			for _, doc := range expectedDocuments {
				found := false
				for _, listedDoc := range resp.Documents {
					if listedDoc.DocID == doc.DocID {
						found = true
						if listedDoc.Text != doc.Text {
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

func DeleteIndex(indexName string) types.Action {
	return types.Action{
		Name: "Delete Index",
		RunFunc: func(ctx context.Context, testContext *types.RAGEngineTestContext) error {
			err := testContext.RAGClient.DeleteIndex(indexName)
			if err != nil {
				return fmt.Errorf("failed to delete index %s: %v", indexName, err)
			}
			return nil
		},
	}
}
