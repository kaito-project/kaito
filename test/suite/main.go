package main

import (
	"context"
	"fmt"
	"log"

	"github.com/kaito-project/kaito/test/e2e/utils"
	"github.com/kaito-project/kaito/test/suite/tests"
	"github.com/kaito-project/kaito/test/suite/types"
	"golang.org/x/sync/errgroup"
)

func main() {
	if err := RunSuite(context.Background()); err != nil {
		log.Fatalf("failed to run test suite: %v", err)
	}
}

func RunSuite(ctx context.Context) error {
	testContext := &types.RAGEngineTestContext{
		Cluster: utils.TestingCluster,
	}

	utils.GetClusterClient(utils.TestingCluster)

	allTests := []types.Test{
		tests.NewRAGEngineTest(),
	}

	concurrentTests := []types.Test{}
	syncTests := []types.Test{}
	for _, test := range allTests {
		if test.CanRunConcurrently() {
			concurrentTests = append(concurrentTests, test)
		} else {
			syncTests = append(syncTests, test)
		}
	}
	errorGroup, ctx := errgroup.WithContext(ctx)

	for _, test := range concurrentTests {
		t := test // capture the current test in the loop variable
		errorGroup.Go(func() error {
			return t.Run(ctx, testContext)
		})
	}

	if err := errorGroup.Wait(); err != nil {
		return fmt.Errorf("failed to run concurrent tests: %w", err)
	}

	for _, test := range syncTests {
		if err := test.Run(ctx, testContext); err != nil {
			return fmt.Errorf("failed to run test %s: %w", test.GetName(), err)
		}
	}

	return nil
}
