package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kopdes/merah-putih/internal/api/middleware"
	"github.com/kopdes/merah-putih/internal/api/response"
)

type UserManagementHandler struct {
	pool *pgxpool.Pool
}

func NewUserManagementHandler(pool *pgxpool.Pool) *UserManagementHandler {
	return &UserManagementHandler{pool: pool}
}

// HandleListUsers mengambil daftar pengguna yang terasosiasi dengan koperasi aktif
func (h *UserManagementHandler) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var userList []map[string]interface{}

	err := middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.nama_lengkap, u.status_aktif, r.kode, u.created_at
			FROM users u
			JOIN user_roles ur ON ur.user_id = u.id AND ur.koperasi_id = $1 AND ur.deleted_at IS NULL
			JOIN roles r ON r.id = ur.role_id
			WHERE u.deleted_at IS NULL
			ORDER BY u.created_at DESC LIMIT 100;
		`, claims.KoperasiID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				id          uuid.UUID
				nama, role  string
				statusAktif bool
				createdAt   time.Time
			)
			if err := rows.Scan(&id, &nama, &statusAktif, &role, &createdAt); err != nil {
				return err
			}
			userList = append(userList, map[string]interface{}{
				"id":           id,
				"nama_lengkap": nama,
				"status_aktif": statusAktif,
				"role":         role,
				"created_at":   createdAt.Format(time.RFC3339),
			})
		}
		return rows.Err()
	})

	if err != nil {
		response.InternalServerError(w, "Gagal mengambil daftar pengguna")
		return
	}

	if userList == nil {
		userList = []map[string]interface{}{}
	}

	response.JSON(w, http.StatusOK, userList, map[string]interface{}{
		"total": len(userList),
	})
}

// HandleGetUserDetail mengambil rincian pengguna
func (h *UserManagementHandler) HandleGetUserDetail(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	targetUserID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID pengguna tidak valid")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	var (
		namaLengkap string
		statusAktif bool
		roleKode    string
		createdAt   time.Time
	)

	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT u.nama_lengkap, u.status_aktif, r.kode, u.created_at
			FROM users u
			LEFT JOIN user_roles ur ON ur.user_id = u.id AND ur.koperasi_id = $1 AND ur.deleted_at IS NULL
			LEFT JOIN roles r ON r.id = ur.role_id
			WHERE u.id = $2 AND u.deleted_at IS NULL;
		`, claims.KoperasiID, targetUserID).Scan(&namaLengkap, &statusAktif, &roleKode, &createdAt)
	})

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, "Pengguna tidak ditemukan")
			return
		}
		response.InternalServerError(w, "Gagal mengambil detail pengguna")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"id":           targetUserID,
		"nama_lengkap": namaLengkap,
		"status_aktif": statusAktif,
		"role":         roleKode,
		"created_at":   createdAt.Format(time.RFC3339),
	})
}

type UpdateUserRoleRequest struct {
	Role string `json:"role"`
}

// HandleUpdateUserRole mengubah atau menetapkan peranan pengguna pada koperasi
func (h *UserManagementHandler) HandleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	targetUserID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "ID pengguna tidak valid")
		return
	}

	var req UpdateUserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "Format payload JSON tidak valid")
		return
	}

	if req.Role == "" {
		response.BadRequest(w, "Nama role baru wajib diisi")
		return
	}

	claims, ok := middleware.GetUserClaims(r.Context())
	if !ok || claims.KoperasiID == uuid.Nil {
		response.Unauthorized(w, "Autentikasi diperlukan")
		return
	}

	ctx := r.Context()
	err = middleware.WithTenantTx(ctx, h.pool, claims.KoperasiID, claims.IsPlatformAdmin, func(tx pgx.Tx) error {
		var roleID uuid.UUID
		err := tx.QueryRow(ctx, "SELECT id FROM roles WHERE kode = $1 OR nama = $1 LIMIT 1;", req.Role).Scan(&roleID)
		if err != nil {
			return fmt.Errorf("role '%s' tidak ditemukan dalam sistem", req.Role)
		}

		// Hapus role lama di koperasi ini
		_, err = tx.Exec(ctx, `
			DELETE FROM user_roles WHERE user_id = $1 AND koperasi_id = $2;
		`, targetUserID, claims.KoperasiID)
		if err != nil {
			return err
		}

		// Tetapkan role baru
		_, err = tx.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, koperasi_id, created_by)
			VALUES ($1, $2, $3, $4);
		`, targetUserID, roleID, claims.KoperasiID, claims.UserID)
		return err
	})

	if err != nil {
		response.InternalServerError(w, fmt.Sprintf("Gagal memperbarui peranan pengguna: %v", err))
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message":   "Peranan pengguna berhasil diperbarui",
		"user_id":   targetUserID,
		"role_baru": req.Role,
	})
}
