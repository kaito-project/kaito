package v1alpha1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAutoIndexer_Validate(t *testing.T) {
	validGit := &GitDataSourceSpec{
		RepositoryURL: "https://github.com/example/repo.git",
		Branch:        "main",
		Paths:         []string{"src", "*.go", "foo/bar", "foo/**"},
		ExcludePaths:  []string{"test", "*.md"},
	}
	validStatic := &StaticDataSourceSpec{
		Endpoints: []string{"https://example.com/file1.txt", "https://example.com/file2.pdf"},
	}
	validSpec := AutoIndexerSpec{
		RAGEngineRef: RAGEngineReference{Name: "rag", Namespace: "default"},
		IndexName:    "my-index",
		DataSource:   DataSourceSpec{Type: DataSourceTypeGitHub, Git: validGit},
	}
	valid := &AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{Name: "ai", Namespace: "default"},
		Spec:       validSpec,
	}
	cases := []struct {
		name    string
		mutate  func(a *AutoIndexer)
		wantErr bool
	}{
		{"valid git", nil, false},
		{"missing ragengine name", func(a *AutoIndexer) { a.Spec.RAGEngineRef.Name = "" }, true},
		{"missing ragengine ns", func(a *AutoIndexer) { a.Spec.RAGEngineRef.Namespace = "" }, true},
		{"missing index name", func(a *AutoIndexer) { a.Spec.IndexName = "" }, true},
		{"invalid index name (bad char)", func(a *AutoIndexer) { a.Spec.IndexName = "bad!name" }, true},
		{"invalid index name (starts with dash)", func(a *AutoIndexer) { a.Spec.IndexName = "-badname" }, true},
		{"invalid index name (ends with dash)", func(a *AutoIndexer) { a.Spec.IndexName = "badname-" }, true},
		{"missing datasource type", func(a *AutoIndexer) { a.Spec.DataSource.Type = "" }, true},
		{"missing git block", func(a *AutoIndexer) { a.Spec.DataSource.Git = nil }, true},
		{"missing git repo url", func(a *AutoIndexer) { a.Spec.DataSource.Git.RepositoryURL = "" }, true},
		{"missing git branch", func(a *AutoIndexer) { a.Spec.DataSource.Git.Branch = "" }, true},
		{"invalid git path", func(a *AutoIndexer) { a.Spec.DataSource.Git.Paths = []string{"foo//bar"} }, true},
		{"invalid git exclude path", func(a *AutoIndexer) { a.Spec.DataSource.Git.ExcludePaths = []string{"foo//bar"} }, true},
		{"valid static", func(a *AutoIndexer) {
			a.Spec.DataSource.Type = DataSourceTypeStatic
			a.Spec.DataSource.Git = nil
			a.Spec.DataSource.Static = validStatic
		}, false},
		{"missing static endpoints", func(a *AutoIndexer) {
			a.Spec.DataSource.Type = DataSourceTypeStatic
			a.Spec.DataSource.Git = nil
			a.Spec.DataSource.Static = &StaticDataSourceSpec{Endpoints: nil}
		}, true},
		{"invalid static endpoint url", func(a *AutoIndexer) {
			a.Spec.DataSource.Type = DataSourceTypeStatic
			a.Spec.DataSource.Git = nil
			a.Spec.DataSource.Static = &StaticDataSourceSpec{Endpoints: []string{"not a url"}}
		}, true},
		{"credentials missing type", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: ""}
		}, true},
		{"credentials secretref missing", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: "SecretRef", SecretRef: nil}
		}, true},
		{"credentials secretref missing name", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: "SecretRef", SecretRef: &SecretKeyRef{Key: "foo"}}
		}, true},
		{"credentials secretref missing key", func(a *AutoIndexer) {
			a.Spec.Credentials = &CredentialsSpec{Type: "SecretRef", SecretRef: &SecretKeyRef{Name: "foo"}}
		}, true},
		{"schedule invalid", func(a *AutoIndexer) {
			s := "bad"
			a.Spec.Schedule = &s
		}, true},
		{"retry policy negative", func(a *AutoIndexer) {
			n := int32(-1)
			a.Spec.RetryPolicy = &RetryPolicySpec{MaxRetries: &n}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := valid.DeepCopy()
			if tc.mutate != nil {
				tc.mutate(a)
			}
			err := a.Validate(context.Background())
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
