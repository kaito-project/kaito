// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/webhook"
)

// Exercise the Knative webhook wrapper used by the workspace admission server,
// including a request without Content-Length. GHSA-2hw4-c9m4-f2fj must be fixed
// before an admission handler decodes the request body.
func TestWebhookRequestBodyLimit(t *testing.T) {
	const bodyLimit = 3 * 1024 * 1024 // Knative's admission request limit.
	const review = `{"apiVersion":"admission.k8s.io/v1","kind":"AdmissionReview","request":{"uid":"body-limit-test"},"padding":"%s"}`
	emptySize := len(fmt.Sprintf(review, ""))

	for _, tc := range []struct {
		name          string
		bodySize      int
		contentLength int64
		wantStatus    int
		wantAdmitted  bool
	}{
		{name: "at limit", bodySize: bodyLimit, contentLength: bodyLimit, wantStatus: http.StatusOK, wantAdmitted: true},
		{name: "over limit", bodySize: bodyLimit + 1, contentLength: bodyLimit + 1, wantStatus: http.StatusBadRequest},
		{name: "unknown length over limit", bodySize: bodyLimit + 1, contentLength: -1, wantStatus: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller := &bodyLimitController{}
			ctx := webhook.WithOptions(context.Background(), webhook.Options{})
			ctx = logging.WithLogger(ctx, zap.NewNop().Sugar())
			server, err := webhook.New(ctx, []interface{}{controller})
			if err != nil {
				t.Fatalf("create webhook: %v", err)
			}
			server.InformersHaveSynced()

			body := fmt.Sprintf(review, strings.Repeat("x", tc.bodySize-emptySize))
			request := httptest.NewRequest(http.MethodPost, controller.Path(), strings.NewReader(body))
			request.ContentLength = tc.contentLength
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)

			if response.Code != tc.wantStatus {
				t.Errorf("response status = %d, want %d", response.Code, tc.wantStatus)
			}
			if controller.called != tc.wantAdmitted {
				t.Errorf("admission handler called = %t, want %t", controller.called, tc.wantAdmitted)
			}
			if !tc.wantAdmitted && !strings.Contains(response.Body.String(), "http: request body too large") {
				t.Errorf("expected body limit error, got %q", response.Body.String())
			}
		})
	}
}

type bodyLimitController struct {
	called bool
}

func (*bodyLimitController) Path() string { return "/body-limit-test" }

func (c *bodyLimitController) Admit(_ context.Context, _ *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	c.called = true
	return &admissionv1.AdmissionResponse{Allowed: true}
}
