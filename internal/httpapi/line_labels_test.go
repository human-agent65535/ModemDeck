package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestLineLabelResourceUpdatesByICCID(t *testing.T) {
	t.Parallel()

	const iccid = "8986010000000000001"
	repository := &fakeRepository{updateLineResult: store.LineSummary{
		ICCID:       iccid,
		LineLabel:   "主卡",
		LineColor:   store.LineColorViolet,
		DeviceAlias: "机房模组",
	}}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	body := bytes.NewBufferString(`{"line_label":"  主卡  ","line_color":"violet"}`)
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/lines/"+url.PathEscape(iccid)+"/label",
		body,
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if repository.updateLineICCID != iccid || repository.updateLineLabel != "  主卡  " {
		t.Fatalf(
			"repository input = (%q, %q), want (%q, %q)",
			repository.updateLineICCID,
			repository.updateLineLabel,
			iccid,
			"  主卡  ",
		)
	}
	if repository.updateLineColor == nil ||
		*repository.updateLineColor != store.LineColorViolet {
		t.Fatalf("repository line color = %v, want violet", repository.updateLineColor)
	}
	var payload lineResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Line.ICCID != iccid ||
		payload.Line.LineLabel != "主卡" ||
		payload.Line.LineColor != store.LineColorViolet {
		t.Fatalf("line = %+v", payload.Line)
	}
}

func TestLineLabelResourceRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	const path = "/api/v1/lines/8986010000000000001/label"
	tests := []struct {
		name       string
		method     string
		body       string
		storeError error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "wrong method",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   "method_not_allowed",
		},
		{
			name:       "missing label",
			method:     http.MethodPatch,
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_argument",
		},
		{
			name:       "invalid label",
			method:     http.MethodPatch,
			body:       `{"line_label":"too long"}`,
			storeError: store.ErrLineValidation,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_line_label",
		},
		{
			name:       "invalid color",
			method:     http.MethodPatch,
			body:       `{"line_label":"主卡","line_color":"magenta"}`,
			storeError: store.ErrLineColorValidation,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_line_color",
		},
		{
			name:       "unknown ICCID",
			method:     http.MethodPatch,
			body:       `{"line_label":"主卡"}`,
			storeError: store.ErrLineNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "line_not_found",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{updateLineError: test.storeError}
			api, err := New(repository, Options{disableAuthentication: true})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, path, bytes.NewBufferString(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			api.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					response.Code,
					test.wantStatus,
					response.Body.String(),
				)
			}
			var payload errorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if payload.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q", payload.Code, test.wantCode)
			}
		})
	}
}
