package v1alpha1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInferenceSet_SupportedVerbs(t *testing.T) {
	tests := []struct {
		name     string
		expected []admissionregistrationv1.OperationType
	}{
		{
			name: "should return Create and Update operations",
			expected: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
				admissionregistrationv1.Update,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := &InferenceSet{}
			got := is.SupportedVerbs()
			assert.Equal(t, tt.expected, got)
			assert.Len(t, got, 2)
			assert.Contains(t, got, admissionregistrationv1.Create)
			assert.Contains(t, got, admissionregistrationv1.Update)
		})
	}
}

func TestInferenceSet_SetDefaults(t *testing.T) {
	tests := []struct {
		name            string
		inferenceset    *InferenceSet
		expectedReplica int
	}{
		{
			name: "replicas should default to 1 when not set",
			inferenceset: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-inferenceset",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					// Replicas omitted - represented as 0 (zero value for int)
					// This simulates what happens when the field is not present in YAML/JSON
					Replicas: 0,
				},
			},
			expectedReplica: 1,
		},
		{
			name: "replicas should not change when already set",
			inferenceset: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-inferenceset",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: 3,
				},
			},
			expectedReplica: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tt.inferenceset.SetDefaults(ctx)
			assert.Equal(t, tt.expectedReplica, tt.inferenceset.Spec.Replicas)
		})
	}
}

func TestInferenceSet_Validate(t *testing.T) {
	tests := []struct {
		name        string
		inferencSet *InferenceSet
		oldIS       *InferenceSet
		wantErr     bool
		errField    string
	}{
		{
			name: "valid DNS1123 label name on create",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: 1,
				},
			},
			oldIS:   nil,
			wantErr: false,
		},
		{
			name: "invalid DNS1123 label name with uppercase",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Invalid-Name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: 1,
				},
			},
			oldIS:    nil,
			wantErr:  true,
			errField: "name",
		},
		{
			name: "invalid DNS1123 label name too long",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "this-is-a-very-long-name-that-exceeds-the-maximum-allowed-length-for-dns",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: 1,
				},
			},
			oldIS:    nil,
			wantErr:  true,
			errField: "name",
		},
		{
			name: "invalid DNS1123 label name with special chars",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid_name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: 1,
				},
			},
			oldIS:    nil,
			wantErr:  true,
			errField: "name",
		},
		{
			name: "valid DNS1123 label name on update",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: 1,
				},
			},
			oldIS: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid replicas value less than 1",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: 0,
				},
			},
			oldIS:    nil,
			wantErr:  true,
			errField: "replicas",
		},
		{
			name: "invalid replicas negative value",
			inferencSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-name",
					Namespace: "default",
				},
				Spec: InferenceSetSpec{
					Replicas: -1,
				},
			},
			oldIS:    nil,
			wantErr:  true,
			errField: "replicas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := tt.inferencSet.Validate(ctx)
			if tt.wantErr {
				assert.NotNil(t, err)
				if tt.errField != "" {
					assert.Contains(t, err.Error(), tt.errField)
				}
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestInferenceSet_validateCreate(t *testing.T) {
	tests := []struct {
		name     string
		is       *InferenceSet
		wantErr  bool
		errField string
	}{
		{
			name: "valid replicas value",
			is: &InferenceSet{
				Spec: InferenceSetSpec{
					Replicas: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid replicas value 0",
			is: &InferenceSet{
				Spec: InferenceSetSpec{
					Replicas: 0,
				},
			},
			wantErr:  true,
			errField: "replicas",
		},
		{
			name: "valid replicas value greater than 1",
			is: &InferenceSet{
				Spec: InferenceSetSpec{
					Replicas: 5,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.is.validateCreate()
			if tt.wantErr {
				assert.NotNil(t, err)
				if tt.errField != "" {
					assert.Contains(t, err.Error(), tt.errField)
				}
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestInferenceSet_validateUpdate(t *testing.T) {
	is := &InferenceSet{}
	old := &InferenceSet{}
	err := is.validateUpdate(old)
	assert.Nil(t, err)
}
