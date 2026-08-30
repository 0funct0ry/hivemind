package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/realtime"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

func userList(s *store.Store, h *realtime.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		var chID *int64
		if cidStr := c.Query("channel_id"); cidStr != "" {
			val, err := strconv.ParseInt(cidStr, 10, 64)
			if err == nil {
				chID = &val
			}
		}
		users, err := s.AutocompleteUsers(c.Request.Context(), c.Query("q"), chID, limit)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list users.")
			return
		}
		data := make([]gin.H, 0, len(users))
		for _, u := range users {
			data = append(data, publicUserOnline(u, h))
		}
		c.JSON(200, gin.H{"data": data})
	}
}
func userGet(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			httpx.Fail(c, 404, "not_found", "User not found.")
			return
		}
		u, err := s.GetUserByID(c.Request.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Fail(c, 404, "not_found", "User not found.")
			return
		}
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not get user.")
			return
		}
		c.JSON(200, gin.H{"user": publicUser(u)})
	}
}
func userMe(s *store.Store, pub realtime.Publisher) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid user update.")
			return
		}
		var in struct {
			DisplayName  *string `json:"display_name"`
			AvatarFileID *string `json:"avatar_file_id"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid user update.")
			return
		}
		// avatar_file_id is tri-state (absent / null / a string), so presence in the
		// raw payload — not just a non-nil pointer — decides whether to touch it at
		// all: an avatar-only PATCH must not implicitly null out on a name-only one.
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(body, &raw)
		_, avatarProvided := raw["avatar_file_id"]

		u, _ := CurrentUser(c)
		if in.DisplayName != nil {
			if err := s.UpdateDisplayName(c.Request.Context(), u.ID, *in.DisplayName); err != nil {
				httpx.Fail(c, 500, "internal_error", "Could not update user.")
				return
			}
		}
		if avatarProvided {
			if err := s.UpdateAvatar(c.Request.Context(), u.ID, in.AvatarFileID); err != nil {
				if err.Error() == "invalid avatar type" {
					httpx.Fail(c, 400, "invalid_avatar_type", "Avatar must be PNG, JPEG, GIF, or WebP.")
					return
				}
				httpx.Fail(c, 400, "invalid_avatar", err.Error())
				return
			}
		}
		updated, err := s.GetUserByID(c.Request.Context(), u.ID)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not get updated user.")
			return
		}
		c.JSON(200, gin.H{"user": publicUser(updated)})

		if in.DisplayName != nil || avatarProvided {
			pub.Publish(realtime.Event{Type: "user.updated", UserID: u.ID})
		}
	}
}
func userCreate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in struct {
			Username    string `json:"username"`
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
			Password    string `json:"password"`
		}
		if c.ShouldBindJSON(&in) != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid user request.")
			return
		}
		if err := auth.ValidatePassword(in.Password); err != nil {
			httpx.Fail(c, 400, "invalid_password", err.Error())
			return
		}
		h, err := auth.HashPassword(in.Password)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not hash password.")
			return
		}
		u, err := s.CreateUser(c.Request.Context(), store.UserInput{Username: in.Username, Email: in.Email, DisplayName: in.DisplayName, PasswordHash: h})
		if err != nil {
			httpx.Fail(c, 400, "invalid_user", err.Error())
			return
		}
		c.JSON(201, gin.H{"user": publicUser(u)})
	}
}
func userDeactivate(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			httpx.Fail(c, 404, "not_found", "User not found.")
			return
		}
		actor, _ := CurrentUser(c)
		if id == actor.ID {
			httpx.Fail(c, 400, "cannot_deactivate_self", "You cannot deactivate yourself.")
			return
		}
		u, err := s.GetUserByID(c.Request.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Fail(c, 404, "not_found", "User not found.")
			return
		}
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not get user.")
			return
		}
		if u.Role == "admin" {
			n, err := s.CountAdmins(c.Request.Context())
			if err != nil {
				httpx.Fail(c, 500, "internal_error", "Could not count administrators.")
				return
			}
			if n <= 1 {
				httpx.Fail(c, 400, "last_admin", "The last administrator cannot be deactivated.")
				return
			}
		}
		if err := s.DeactivateAndOrphanWebhooks(c.Request.Context(), id); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not deactivate user.")
			return
		}
		c.Status(204)
	}
}
