package api

import (
	"database/sql"
	"errors"
	"strconv"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/auth"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

func userList(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		users, err := s.ListUsers(c, c.Query("q"), limit, false)
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not list users.")
			return
		}
		data := make([]gin.H, 0, len(users))
		for _, u := range users {
			data = append(data, publicUser(u))
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
		u, err := s.GetUserByID(c, id)
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
func userMe(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in struct {
			DisplayName string `json:"display_name"`
		}
		if c.ShouldBindJSON(&in) != nil {
			httpx.Fail(c, 400, "invalid_request", "Invalid user update.")
			return
		}
		u, _ := CurrentUser(c)
		if err := s.UpdateDisplayName(c, u.ID, in.DisplayName); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not update user.")
			return
		}
		u.DisplayName = in.DisplayName
		c.JSON(200, gin.H{"user": publicUser(u)})
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
		u, err := s.CreateUser(c, store.UserInput{Username: in.Username, Email: in.Email, DisplayName: in.DisplayName, PasswordHash: h})
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
		u, err := s.GetUserByID(c, id)
		if errors.Is(err, sql.ErrNoRows) {
			httpx.Fail(c, 404, "not_found", "User not found.")
			return
		}
		if err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not get user.")
			return
		}
		if u.Role == "admin" {
			n, err := s.CountAdmins(c)
			if err != nil {
				httpx.Fail(c, 500, "internal_error", "Could not count administrators.")
				return
			}
			if n <= 1 {
				httpx.Fail(c, 400, "last_admin", "The last administrator cannot be deactivated.")
				return
			}
		}
		if err := s.Deactivate(c, id); err != nil {
			httpx.Fail(c, 500, "internal_error", "Could not deactivate user.")
			return
		}
		c.Status(204)
	}
}
