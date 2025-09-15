// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package manifests

import (
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

func createTestAutoIndexer(name, namespace string, schedule *string) *kaitov1alpha1.AutoIndexer {
	return &kaitov1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: kaitov1alpha1.AutoIndexerSpec{
			RAGEngineRef: metav1.ObjectMeta{
				Name:      "test-ragengine",
				Namespace: namespace,
			},
			IndexName: "test-index",
			DataSource: kaitov1alpha1.DataSourceSpec{
				Type: kaitov1alpha1.DataSourceTypeGitHub,
				Git: &kaitov1alpha1.GitDataSourceSpec{
					Repository: "https://github.com/example/test-repo",
					Branch:     "main",
					Paths:      []string{"docs/"},
				},
			},
			Schedule: schedule,
			Credentials: &kaitov1alpha1.CredentialsSpec{
				Type: kaitov1alpha1.CredentialTypeSecretRef,
				SecretRef: &kaitov1alpha1.SecretKeyRef{
					Name: "github-credentials",
					Key:  "token",
				},
			},
			RetryPolicy: &kaitov1alpha1.RetryPolicySpec{
				MaxRetries:      3,
				BackoffStrategy: "exponential",
			},
		},
	}
}

func TestGenerateIndexingJobManifest(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	config := GetDefaultJobConfig(autoIndexer, JobTypeOneTime)
	job := GenerateIndexingJobManifest(config)

	// Validate basic job properties
	if job == nil {
		t.Fatal("Generated job is nil")
	}

	if job.Name != config.JobName {
		t.Errorf("Expected job name %s, got %s", config.JobName, job.Name)
	}

	if job.Namespace != autoIndexer.Namespace {
		t.Errorf("Expected job namespace %s, got %s", autoIndexer.Namespace, job.Namespace)
	}

	// Validate labels
	expectedLabels := getJobLabels(autoIndexer, JobTypeOneTime)
	for key, expectedValue := range expectedLabels {
		if value, exists := job.Labels[key]; !exists || value != expectedValue {
			t.Errorf("Expected label %s=%s, got %s (exists: %t)", key, expectedValue, value, exists)
		}
	}

	// Validate owner reference
	if len(job.OwnerReferences) != 1 {
		t.Fatalf("Expected 1 owner reference, got %d", len(job.OwnerReferences))
	}

	ownerRef := job.OwnerReferences[0]
	if ownerRef.Name != autoIndexer.Name {
		t.Errorf("Expected owner reference name %s, got %s", autoIndexer.Name, ownerRef.Name)
	}

	// Validate job spec
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("Expected restart policy Never, got %s", job.Spec.Template.Spec.RestartPolicy)
	}

	if len(job.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(job.Spec.Template.Spec.Containers))
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Name != "autoindexer" {
		t.Errorf("Expected container name 'autoindexer', got %s", container.Name)
	}

	if container.Image != AutoIndexerImage {
		t.Errorf("Expected container image %s, got %s", AutoIndexerImage, container.Image)
	}

	// Validate environment variables
	envVarExists := func(name string) bool {
		for _, env := range container.Env {
			if env.Name == name {
				return true
			}
		}
		return false
	}

	expectedEnvVars := []string{
		EnvIndexName,
		EnvRAGEngineEndpoint,
		EnvDataSourceType,
		EnvDataSourceConfig,
		EnvCredentialsConfig,
		EnvRetryPolicy,
	}

	for _, envVar := range expectedEnvVars {
		if !envVarExists(envVar) {
			t.Errorf("Expected environment variable %s not found", envVar)
		}
	}

	// Validate volumes and volume mounts for credentials
	if len(job.Spec.Template.Spec.Volumes) == 0 {
		t.Error("Expected volumes for credentials, but none found")
	}

	if len(container.VolumeMounts) == 0 {
		t.Error("Expected volume mounts for credentials, but none found")
	}
}

func TestGenerateIndexingCronJobManifest(t *testing.T) {
	schedule := "0 2 * * *"
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", &schedule)

	config := GetDefaultJobConfig(autoIndexer, JobTypeScheduled)
	cronJob := GenerateIndexingCronJobManifest(config)

	// Validate basic cronjob properties
	if cronJob == nil {
		t.Fatal("Generated cronjob is nil")
	}

	if cronJob.Name != config.JobName {
		t.Errorf("Expected cronjob name %s, got %s", config.JobName, cronJob.Name)
	}

	if cronJob.Namespace != autoIndexer.Namespace {
		t.Errorf("Expected cronjob namespace %s, got %s", autoIndexer.Namespace, cronJob.Namespace)
	}

	// Validate schedule
	if cronJob.Spec.Schedule != schedule {
		t.Errorf("Expected schedule %s, got %s", schedule, cronJob.Spec.Schedule)
	}

	// Validate concurrency policy
	if cronJob.Spec.ConcurrencyPolicy != batchv1.ForbidConcurrent {
		t.Errorf("Expected concurrency policy ForbidConcurrent, got %s", cronJob.Spec.ConcurrencyPolicy)
	}

	// Validate job template
	jobTemplate := cronJob.Spec.JobTemplate
	if len(jobTemplate.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Expected 1 container in job template, got %d", len(jobTemplate.Spec.Template.Spec.Containers))
	}

	container := jobTemplate.Spec.Template.Spec.Containers[0]
	if container.Name != "autoindexer" {
		t.Errorf("Expected container name 'autoindexer', got %s", container.Name)
	}
}

func TestGenerateIndexingCronJobManifest_NoSchedule(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	config := GetDefaultJobConfig(autoIndexer, JobTypeScheduled)
	cronJob := GenerateIndexingCronJobManifest(config)

	// Should return nil when no schedule is provided
	if cronJob != nil {
		t.Error("Expected nil cronjob when no schedule is provided")
	}
}

func TestGenerateJobName(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	// Test one-time job name
	oneTimeJobName := GenerateJobName(autoIndexer, JobTypeOneTime)
	if !strings.HasPrefix(oneTimeJobName, "test-autoindexer-job-") {
		t.Errorf("One-time job name should start with 'test-autoindexer-job-', got %s", oneTimeJobName)
	}

	// Test scheduled job name
	scheduledJobName := GenerateJobName(autoIndexer, JobTypeScheduled)
	expectedScheduledName := "test-autoindexer-cronjob"
	if scheduledJobName != expectedScheduledName {
		t.Errorf("Expected scheduled job name %s, got %s", expectedScheduledName, scheduledJobName)
	}
}

func TestValidateJobConfig(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	// Valid config
	validConfig := JobConfig{
		AutoIndexer: autoIndexer,
		JobName:     "test-job",
		JobType:     JobTypeOneTime,
	}

	if err := ValidateJobConfig(validConfig); err != nil {
		t.Errorf("Valid config should not produce error, got: %v", err)
	}

	// Invalid config - nil AutoIndexer
	invalidConfig := JobConfig{
		AutoIndexer: nil,
		JobName:     "test-job",
		JobType:     JobTypeOneTime,
	}

	if err := ValidateJobConfig(invalidConfig); err == nil {
		t.Error("Config with nil AutoIndexer should produce error")
	}

	// Invalid config - empty job name
	invalidConfig.AutoIndexer = autoIndexer
	invalidConfig.JobName = ""

	if err := ValidateJobConfig(invalidConfig); err == nil {
		t.Error("Config with empty job name should produce error")
	}

	// Invalid config - invalid job type
	invalidConfig.JobName = "test-job"
	invalidConfig.JobType = "invalid"

	if err := ValidateJobConfig(invalidConfig); err == nil {
		t.Error("Config with invalid job type should produce error")
	}

	// Invalid config - scheduled job without schedule
	schedule := "0 2 * * *"
	autoIndexerWithSchedule := createTestAutoIndexer("test-autoindexer", "default", &schedule)
	autoIndexerWithoutSchedule := createTestAutoIndexer("test-autoindexer", "default", nil)

	invalidConfig.AutoIndexer = autoIndexerWithoutSchedule
	invalidConfig.JobType = JobTypeScheduled

	if err := ValidateJobConfig(invalidConfig); err == nil {
		t.Error("Scheduled job without schedule should produce error")
	}

	// Valid scheduled config
	validScheduledConfig := JobConfig{
		AutoIndexer: autoIndexerWithSchedule,
		JobName:     "test-cronjob",
		JobType:     JobTypeScheduled,
	}

	if err := ValidateJobConfig(validScheduledConfig); err != nil {
		t.Errorf("Valid scheduled config should not produce error, got: %v", err)
	}
}

func TestGetDefaultJobConfig(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	config := GetDefaultJobConfig(autoIndexer, JobTypeOneTime)

	if config.AutoIndexer != autoIndexer {
		t.Error("Default config should use provided AutoIndexer")
	}

	if config.JobType != JobTypeOneTime {
		t.Errorf("Expected job type %s, got %s", JobTypeOneTime, config.JobType)
	}

	if config.Image != AutoIndexerImage {
		t.Errorf("Expected image %s, got %s", AutoIndexerImage, config.Image)
	}

	if config.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("Expected pull policy %s, got %s", corev1.PullIfNotPresent, config.ImagePullPolicy)
	}

	if config.JobName == "" {
		t.Error("Default config should generate a job name")
	}
}

func TestGenerateRAGEngineEndpoint(t *testing.T) {
	autoIndexer := createTestAutoIndexer("test-autoindexer", "default", nil)

	endpoint := generateRAGEngineEndpoint(autoIndexer)
	expected := "http://test-ragengine.default.svc.cluster.local:80"

	if endpoint != expected {
		t.Errorf("Expected endpoint %s, got %s", expected, endpoint)
	}

	// Test with different namespace in RAGEngineRef
	autoIndexer.Spec.RAGEngineRef.Namespace = "other-namespace"
	endpoint = generateRAGEngineEndpoint(autoIndexer)
	expected = "http://test-ragengine.other-namespace.svc.cluster.local:80"

	if endpoint != expected {
		t.Errorf("Expected endpoint %s, got %s", expected, endpoint)
	}

	// Test with empty namespace in RAGEngineRef (should use AutoIndexer namespace)
	autoIndexer.Spec.RAGEngineRef.Namespace = ""
	endpoint = generateRAGEngineEndpoint(autoIndexer)
	expected = "http://test-ragengine.default.svc.cluster.local:80"

	if endpoint != expected {
		t.Errorf("Expected endpoint %s, got %s", expected, endpoint)
	}
}

func TestGenerateDataSourceConfig(t *testing.T) {
	// Test Git data source
	gitDataSource := kaitov1alpha1.DataSourceSpec{
		Type: kaitov1alpha1.DataSourceTypeGitHub,
		Git: &kaitov1alpha1.GitDataSourceSpec{
			Repository: "https://github.com/example/test-repo",
			Branch:     "main",
			Paths:      []string{"docs/"},
		},
	}

	config, err := generateDataSourceConfig(gitDataSource)
	if err != nil {
		t.Fatalf("Failed to generate Git data source config: %v", err)
	}

	if config == "" {
		t.Error("Generated config should not be empty")
	}

	// Test Static data source
	staticDataSource := kaitov1alpha1.DataSourceSpec{
		Type: kaitov1alpha1.DataSourceTypeStatic,
		Static: &kaitov1alpha1.StaticDataSourceSpec{
			Endpoint: "https://api.example.com/docs",
		},
	}

	config, err = generateDataSourceConfig(staticDataSource)
	if err != nil {
		t.Fatalf("Failed to generate Static data source config: %v", err)
	}

	if config == "" {
		t.Error("Generated config should not be empty")
	}
}

func TestGetBackoffLimit(t *testing.T) {
	// Test with retry policy
	retryPolicy := &kaitov1alpha1.RetryPolicySpec{
		MaxRetries: 5,
	}

	backoffLimit := getBackoffLimit(retryPolicy)
	if *backoffLimit != 5 {
		t.Errorf("Expected backoff limit 5, got %d", *backoffLimit)
	}

	// Test without retry policy (should use default)
	backoffLimit = getBackoffLimit(nil)
	if *backoffLimit != 3 {
		t.Errorf("Expected default backoff limit 3, got %d", *backoffLimit)
	}

	// Test with zero max retries (should use default)
	retryPolicy.MaxRetries = 0
	backoffLimit = getBackoffLimit(retryPolicy)
	if *backoffLimit != 3 {
		t.Errorf("Expected default backoff limit 3 for zero retries, got %d", *backoffLimit)
	}
}
