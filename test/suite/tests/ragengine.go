package tests

import (
	"github.com/kaito-project/kaito/test/suite/clients/ragengine"
	"github.com/kaito-project/kaito/test/suite/tests/actions"
	"github.com/kaito-project/kaito/test/suite/types"
)

type RAGEngineTest struct {
	types.BaseTest
	IndexName string
	Documents []*ragengine.RAGDocument
}

func NewRAGEngineTest() types.Test {
	test := RAGEngineTest{
		IndexName: "e2e_test_index",
		Documents: []*ragengine.RAGDocument{
			{
				Text:     "Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster",
				Metadata: map[string]any{"author": "kaito", "category": "kaito"},
			},
		},
	}

	test.BaseTest = types.BaseTest{
		Name:            "RAGEngine E2E Test",
		Description:     "End-to-end test for RAGEngine functionality",
		RunConcurrently: true,
		Actions: []types.Action{
			actions.GetRagClient("kaito-ragengine", ""),
			actions.CreateIndex(test.IndexName, test.Documents),
			actions.ListIndexes([]string{test.IndexName}),
			actions.ListDocumentsInIndex(test.IndexName, 10, 0, 0, nil, test.Documents),
			actions.QueryIndex(test.IndexName, "what is kaito?", 1, 50, 0),
			actions.DeleteIndex(test.IndexName),
		},
	}

	return test
}
