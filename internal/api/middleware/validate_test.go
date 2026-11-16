package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const validateSpec = `
openapi: 3.1.0
info:
  title: t
  version: "1"
paths:
  /things/{id}:
    post:
      operationId: createThing
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
        - name: limit
          in: query
          schema:
            type: integer
            minimum: 1
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              properties:
                name:
                  type: string
                  minLength: 1
                size:
                  type: integer
                kind:
                  type: string
                  enum:
                    - a
                    - b
              required:
                - name
                - size
      responses:
        "204":
          description: ok
`

func TestValidateRequest(t *testing.T) {
	const id = "7"

	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
		wantCode    int
		wantBody    string
	}{
		{name: "valid", method: http.MethodPost, path: "/api/things/" + id, body: `{"name":"a","size":1}`, wantCode: http.StatusNoContent},
		{
			name: "every missing field", method: http.MethodPost, path: "/api/things/" + id, body: `{}`, wantCode: http.StatusBadRequest,
			wantBody: `{"code": "invalid_request", "message": "request is not valid", "params": [
				{"field": "name", "message": "name is required"}, {"field": "size", "message": "size is required"}]}`,
		},
		{
			name: "unknown field", method: http.MethodPost, path: "/api/things/" + id, body: `{"name":"a","size":1,"x":1}`, wantCode: http.StatusBadRequest,
			wantBody: `{"code": "invalid_request", "message": "x is not allowed", "params": [{"field": "x", "message": "x is not allowed"}]}`,
		},
		{
			name: "wrong type", method: http.MethodPost, path: "/api/things/" + id, body: `{"name":"a","size":"big"}`, wantCode: http.StatusBadRequest,
			wantBody: `{"code": "invalid_request", "message": "size is not valid: got string, want integer", "params": [{"field": "size", "message": "size is not valid: got string, want integer"}]}`,
		},
		{
			name: "body not an object", method: http.MethodPost, path: "/api/things/" + id, body: `[]`, wantCode: http.StatusBadRequest,
			wantBody: `{"code": "invalid_request", "message": "body is not valid: got array, want object", "params": [{"field": "body", "message": "body is not valid: got array, want object"}]}`,
		},
		{
			name: "bad path parameter", method: http.MethodPost, path: "/api/things/nope", body: `{"name":"a","size":1}`, wantCode: http.StatusBadRequest,
			wantBody: `{"code": "invalid_request", "message": "value nope: an invalid integer: invalid syntax", "params": [{"field": "id", "message": "value nope: an invalid integer: invalid syntax"}]}`,
		},
		{
			name: "query parameter out of range", method: http.MethodPost, path: "/api/things/" + id + "?limit=0", body: `{"name":"a","size":1}`, wantCode: http.StatusBadRequest,
			wantBody: `{"code": "invalid_request", "message": "limit is not valid: minimum: got 0, want 1", "params": [{"field": "limit", "message": "limit is not valid: minimum: got 0, want 1"}]}`,
		},
		{
			name: "value not in the enum", method: http.MethodPost, path: "/api/things/" + id, body: `{"name":"a","size":1,"kind":"c"}`, wantCode: http.StatusBadRequest,
			wantBody: `{"code": "invalid_request", "message": "kind is not valid: value must be one of 'a', 'b'", "params": [{"field": "kind", "message": "kind is not valid: value must be one of 'a', 'b'"}]}`,
		},
		{name: "wrong content type", method: http.MethodPost, path: "/api/things/" + id, contentType: "text/plain", body: `{"name":"a","size":1}`, wantCode: http.StatusBadRequest},
		{
			name: "unknown path", method: http.MethodGet, path: "/api/nothing", wantCode: http.StatusNotFound,
			wantBody: `{"code": "not_found", "message": "no such operation"}`,
		},
		{
			name: "wrong method", method: http.MethodGet, path: "/api/things/" + id, wantCode: http.StatusMethodNotAllowed,
			wantBody: `{"code": "invalid_request", "message": "method not allowed"}`,
		},
		{
			name: "body too large", method: http.MethodPost, path: "/api/things/" + id, body: `{"name":"` + strings.Repeat("a", 64) + `","size":1}`, wantCode: http.StatusRequestEntityTooLarge,
			wantBody: `{"code": "payload_too_large", "message": "request body is too large"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}), LimitBody(32), ValidateRequest(loadSpec(t, []byte(validateSpec)), "/api"))
			r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				contentType := tt.contentType
				if contentType == "" {
					contentType = "application/json"
				}

				r.Header.Set("Content-Type", contentType)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			assert.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}
