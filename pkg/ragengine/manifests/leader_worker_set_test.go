package manifests

import (
	"context"
	"testing"

	"github.com/kaito-project/kaito/pkg/utils/test"
	v1 "k8s.io/api/core/v1"
)

func TestGenerateRAGLWSManifest(t *testing.T) {
	// Mocking the RAGEngine object for the test
	ragEngine := test.MockRAGEngineWithPreset
	manifest := GenerateRAGLWSManifest(context.TODO(), ragEngine, test.MockRAGEngineWithPresetHash,
		"",                            // imageName
		nil,                           // imagePullSecretRefs
		*ragEngine.Spec.Compute.Count, // replicas
		nil,                           // commands
		nil,                           // containerPorts
		nil,                           // livenessProbe
		nil,                           // readinessProbe
		v1.ResourceRequirements{},
		nil, // tolerations
		nil, // volumes
		nil, // volumeMount
	)
	// Extract node selector requirements from the deployment manifest
	nodeReq := manifest.Spec.LeaderWorkerTemplate.LeaderTemplate.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions

	// Validate if the node requirements match the RAGEngine's label selector
	for key, value := range ragEngine.Spec.Compute.LabelSelector.MatchLabels {
		if !kvInNodeRequirement(key, value, nodeReq) {
			t.Errorf("Node affinity requirements are wrong for key %s and value %s", key, value)
		}
	}
}
