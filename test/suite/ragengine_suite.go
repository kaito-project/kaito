package suite

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kaito-project/kaito/test/e2e/utils"
	"github.com/kaito-project/kaito/test/suite/clients/ragengine"
	ragtests "github.com/kaito-project/kaito/test/suite/ragengine"
	"github.com/kaito-project/kaito/test/suite/types"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func RunRAGEngineSuite(ctx context.Context, ragEngineName, ragEngineNamespace string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Needed as some utils functions use Gomega assertions
	RegisterFailHandler(Fail)

	testContext := &types.RAGEngineTestContext{
		Cluster: utils.TestingCluster,
	}

	utils.GetClusterClient(utils.TestingCluster)
	err := initRagClient(ctx, testContext, ragEngineName, ragEngineNamespace)
	if err != nil {
		return fmt.Errorf("failed to initialize RAG client: %w", err)
	}

	allTests := []types.Test{
		ragtests.NewBasicRAGEngineTest(ragEngineName, ragEngineNamespace),
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
			if err := t.Run(ctx, logger.With("test", t.GetName()), testContext); err != nil {
				return fmt.Errorf("failed to run test %s: %w", t.GetName(), err)
			}
			return nil
		})
	}

	if err := errorGroup.Wait(); err != nil {
		return fmt.Errorf("failed to run concurrent tests: %w", err)
	}

	for _, test := range syncTests {
		if err := test.Run(ctx, logger.With("test", test.GetName()), testContext); err != nil {
			return fmt.Errorf("failed to run test %s: %w", test.GetName(), err)
		}
	}

	return nil
}

func initRagClient(ctx context.Context, testContext *types.RAGEngineTestContext, ragEngineName, ragEngineNamespace string) error {
	k8sClient, err := utils.GetK8sClientset()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes clientset: %v", err)
	}

	ragSvc, err := k8sClient.CoreV1().Services(ragEngineNamespace).Get(ctx, ragEngineName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get RAGEngine service %s in namespace %s: %v", ragEngineName, ragEngineNamespace, err)
	}
	if ragSvc == nil {
		return fmt.Errorf("RAGEngine service %s not found in namespace %s", ragEngineName, ragEngineNamespace)
	}

	if ragSvc.Spec.ClusterIP == "" {
		return fmt.Errorf("RAGEngine service %s in namespace %s does not have a ClusterIP", ragEngineName, ragEngineNamespace)
	}

	testContext.RAGClient = ragengine.NewClient(ragengine.RAGEngineConfig{
		URL: fmt.Sprintf("http://%s", ragSvc.Spec.ClusterIP),
	})

	return nil
}
