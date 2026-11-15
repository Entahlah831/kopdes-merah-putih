package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
