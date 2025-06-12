package main

import (
	"context"
	"flag"
	"log"

	"github.com/kaito-project/kaito/test/suite"
)

func main() {
	var ragEngineName string
	var ragEngineNamespace string

	flag.StringVar(&ragEngineName, "rag-engine-name", "kaito-ragengine", "Name of the RAG Engine")
	flag.StringVar(&ragEngineNamespace, "rag-engine-namespace", "default", "Namespace of the RAG Engine")
	flag.Parse()

	if ragEngineName == "" {
		log.Fatal("rag-engine-name must be provided")
	}
	if ragEngineNamespace == "" {
		log.Fatal("rag-engine-namespace must be provided")
	}

	ctx := context.Background()
	if err := suite.RunRAGEngineSuite(ctx, ragEngineName, ragEngineNamespace); err != nil {
		log.Fatalf("failed to run RAG Engine suite: %v", err)
	}

	log.Println("RAG Engine suite completed successfully")
}
