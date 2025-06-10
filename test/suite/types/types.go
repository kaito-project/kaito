package types

import (
	"context"
	"fmt"

	"github.com/kaito-project/kaito/test/e2e/utils"
	"github.com/kaito-project/kaito/test/suite/clients/ragengine"
)

type RAGEngineTestContext struct {
	Cluster   *utils.Cluster
	RAGClient *ragengine.Client
}

type TestFunc func(ctx context.Context, testContext *RAGEngineTestContext) error

type Action struct {
	Name        string
	SetupFunc   TestFunc
	RunFunc     TestFunc
	CleanupFunc TestFunc
}

type Test interface {
	GetName() string
	GetDescription() string
	CanRunConcurrently() bool
	Run(ctx context.Context, testContext *RAGEngineTestContext) error
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

func (t BaseTest) Run(ctx context.Context, testContext *RAGEngineTestContext) error {
	for _, action := range t.Actions {
		if action.SetupFunc == nil {
			continue
		}

		if err := action.SetupFunc(ctx, testContext); err != nil {
			return fmt.Errorf("failed to setup test %s action %s: %w", t.Name, action.Name, err)
		}
	}

	for _, action := range t.Actions {
		if err := action.RunFunc(ctx, testContext); err != nil {
			return fmt.Errorf("failed to run test %s action %s: %w", t.Name, action.Name, err)
		}
	}

	for _, action := range t.Actions {
		if action.CleanupFunc == nil {
			continue
		}

		if err := action.CleanupFunc(ctx, testContext); err != nil {
			return fmt.Errorf("failed to cleanup test %s action %s: %w", t.Name, action.Name, err)
		}
	}

	return nil
}
