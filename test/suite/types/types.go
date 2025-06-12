package types

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kaito-project/kaito/test/e2e/utils"
	"github.com/kaito-project/kaito/test/suite/clients/ragengine"
)

type RAGEngineTestContext struct {
	Cluster   *utils.Cluster
	RAGClient *ragengine.Client
}

type TestFunc func(ctx context.Context, logger *slog.Logger, testContext *RAGEngineTestContext) error

type Action struct {
	Name        string
	RunFunc     TestFunc
	CleanupFunc TestFunc
}

type Test interface {
	GetName() string
	GetDescription() string
	CanRunConcurrently() bool
	Run(ctx context.Context, logger *slog.Logger, testContext *RAGEngineTestContext) error
}

type BaseTest struct {
	Name            string
	Description     string
	RunConcurrently bool
	Actions         []Action
}

func (t BaseTest) GetName() string {
	return t.Name
}

func (t BaseTest) GetDescription() string {
	return t.Description
}

func (t BaseTest) CanRunConcurrently() bool {
	return t.RunConcurrently
}

// Run executes the test by running all actions in sequence.
// It first runs setup actions, then the main test actions, and finally cleanup actions.
// If a setup action fails, the test will not proceed to the main actions.
// If a main action fails, it will stop executing further test actions and run cleanup actions.
func (t BaseTest) Run(ctx context.Context, logger *slog.Logger, testContext *RAGEngineTestContext) error {
	logger = logger.With("test", t.Name)

	if testContext == nil {
		return fmt.Errorf("test context cannot be nil for test %s", t.Name)
	}

	var failedTest, failedAction string
	var err error
	for _, action := range t.Actions {
		if action.RunFunc == nil {
			logger.Error("action run function is nil", "action", action.Name)
			return fmt.Errorf("action run function is nil for action %s in test %s", action.Name, t.Name)
		}

		if err = action.RunFunc(ctx, logger.With("action", action.Name, "stage", "run"), testContext); err != nil {
			failedTest = t.Name
			failedAction = action.Name
			logger.Error("failed to run test action", "action", action.Name, "error", err)
			break
		}
	}

	// Cleanup actions should always run and not fail if the test failed or was not run.
	for _, action := range t.Actions {
		if action.CleanupFunc == nil {
			continue
		}

		if err := action.CleanupFunc(ctx, logger.With("action", action.Name, "stage", "cleanup"), testContext); err != nil {
			logger.Error("failed to run cleanup action", "action", action.Name, "error", err)
			return fmt.Errorf("failed to cleanup test %s action %s: %w", t.Name, action.Name, err)
		}
	}

	if err != nil {
		return fmt.Errorf("test %s failed at action %s: %w", failedTest, failedAction, err)
	}

	return nil
}
