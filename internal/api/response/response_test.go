package response

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestJSONResponse(t *testing.T) {
	t.Run("without meta", func(t *testing.T) {
		rec := httptest.NewRecorder()
		JSON(rec, http.StatusCreated, map[string]string{"id": "123"})

		if rec.Code != http.StatusCreated {
			t.Fatalf("status=%d, ingin %d", rec.Code, http.StatusCreated)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Fatalf("Content-Type=%q, ingin application/json; charset=utf-8", ct)
		}

		var got APIResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("gagal decode JSON: %v", err)
		}
		if !got.Success || got.Error != nil || got.Meta != nil {
			t.Fatalf("respons tidak sesuai: %+v", got)
		}
	})

	t.Run("with meta", func(t *testing.T) {
		rec := httptest.NewRecorder()
		JSON(rec, http.StatusOK, []string{"a", "b"}, map[string]int{"total": 2})

		var got APIResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("gagal decode JSON: %v", err)
		}
		if !got.Success || got.Meta == nil {
			t.Fatalf("meta harus terisi: %+v", got)
		}
	})
}

func TestErrorHelpers(t *testing.T) {
	cases := []struct {
		name       string
		invoke     func(http.ResponseWriter)
		wantStatus int
		wantCode   string
	}{
		{"BadRequest", func(w http.ResponseWriter) { BadRequest(w, "pesan uji", map[string]string{"field": "email"}) }, http.StatusBadRequest, "BAD_REQUEST"},
		{"Unauthorized", func(w http.ResponseWriter) { Unauthorized(w, "pesan uji") }, http.StatusUnauthorized, "UNAUTHORIZED"},
		{"Forbidden", func(w http.ResponseWriter) { Forbidden(w, "pesan uji") }, http.StatusForbidden, "FORBIDDEN"},
		{"NotFound", func(w http.ResponseWriter) { NotFound(w, "pesan uji") }, http.StatusNotFound, "NOT_FOUND"},
		{"Conflict", func(w http.ResponseWriter) { Conflict(w, "pesan uji", map[string]string{"slot": "penuh"}) }, http.StatusConflict, "CONFLICT"},
		{"InternalServerError", func(w http.ResponseWriter) { InternalServerError(w, "pesan uji") }, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.invoke(rec)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, ingin %d", rec.Code, tc.wantStatus)
			}
			var got APIResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("gagal decode JSON: %v", err)
			}
			if got.Success || got.Error == nil {
				t.Fatalf("envelope error harus terisi: %+v", got)
			}
			if got.Error.Code != tc.wantCode || got.Error.Message != "pesan uji" {
				t.Fatalf("error=%+v, ingin code=%s message=pesan uji", got.Error, tc.wantCode)
			}
		})
	}
}

func TestWriteServiceError_PgErrors(t *testing.T) {
	cases := []struct {
		code       string
		wantStatus int
		wantErr    string
	}{
		{"23505", http.StatusConflict, "CONFLICT"},
		{"22021", http.StatusBadRequest, "BAD_REQUEST"},
		{"22001", http.StatusBadRequest, "BAD_REQUEST"},
		{"22P02", http.StatusBadRequest, "BAD_REQUEST"},
		{"23503", http.StatusBadRequest, "BAD_REQUEST"},
		{"23514", http.StatusBadRequest, "BAD_REQUEST"},
		{"53300", http.StatusInternalServerError, "INTERNAL_SERVER_ERROR"},
	}

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			pgErr := &pgconn.PgError{
				Code:           tc.code,
				Message:        "SECRET_PG_INTERNAL_MSG",
				Detail:         "SECRET_PG_DETAIL_ROW",
				TableName:      "users",
				ConstraintName: "uq_users_email",
			}
			WriteServiceError(rec, fmt.Errorf("wrap: %w", pgErr))

			if rec.Code != tc.wantStatus {
				t.Fatalf("code=%s status=%d, ingin %d", tc.code, rec.Code, tc.wantStatus)
			}
			body := rec.Body.String()
			for _, leak := range []string{"SECRET_PG_INTERNAL_MSG", "SECRET_PG_DETAIL_ROW", "uq_users_email"} {
				if strings.Contains(body, leak) {
					t.Fatalf("detail internal PostgreSQL %q bocor ke respons HTTP: %s", leak, body)
				}
			}
			var got APIResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("gagal decode JSON: %v", err)
			}
			if got.Error == nil || got.Error.Code != tc.wantErr {
				t.Fatalf("error=%+v, ingin code=%s", got.Error, tc.wantErr)
			}
		})
	}
}
