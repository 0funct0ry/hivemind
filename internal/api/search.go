package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/0funct0ry/hivemind/internal/api/httpx"
	"github.com/0funct0ry/hivemind/internal/store"
	"github.com/gin-gonic/gin"
)

// search handles GET /api/v1/search
func search(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		currentUser, ok := CurrentUser(c)
		if !ok {
			httpx.Fail(c, 401, "unauthenticated", "Authentication required.")
			return
		}

		qText := c.Query("q")
		channelStr := c.Query("channel")
		fromStr := c.Query("from")
		hasStr := c.Query("has")
		beforeStr := c.Query("before")
		limitStr := c.Query("limit")

		var channelID *int64
		if channelStr != "" {
			cid, err := resolveChannelID(c.Request.Context(), s, channelStr)
			if err != nil {
				httpx.Fail(c, 404, "channel_not_found", "Channel not found.")
				return
			}
			channelID = &cid
		}

		var fromUserID *int64
		if fromStr != "" {
			var username string
			if fromStr[0] == '@' {
				username = fromStr[1:]
			} else {
				username = fromStr
			}
			u, err := s.GetUserByLogin(c.Request.Context(), username)
			if err != nil {
				c.JSON(200, gin.H{
					"data":        []store.Hit{},
					"has_more":    false,
					"next_before": nil,
				})
				return
			}
			fromUserID = &u.ID
		}

		var has *string
		if hasStr != "" {
			if hasStr == "file" || hasStr == "image" || hasStr == "link" {
				has = &hasStr
			}
		}

		var before *int64
		if beforeStr != "" {
			b, err := strconv.ParseInt(beforeStr, 10, 64)
			if err == nil {
				before = &b
			}
		}

		limit := 50
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}

		query := store.SearchQuery{
			Text:       qText,
			ChannelID:  channelID,
			FromUserID: fromUserID,
			Has:        has,
			Before:     before,
			Limit:      limit + 1,
		}

		hits, err := s.Search(c.Request.Context(), currentUser.ID, query)
		if err != nil {
			if err.Error() == "empty_search_query" {
				httpx.Fail(c, 400, "invalid_search_query", "Search query cannot be empty.")
				return
			}
			httpx.Fail(c, 400, "invalid_search_query", "Invalid search query syntax.")
			return
		}

		hasMore := len(hits) > limit
		var nextBefore *string
		if hasMore {
			hits = hits[:limit]
			lastID := strconv.FormatInt(hits[len(hits)-1].Message.ID, 10)
			nextBefore = &lastID
		}

		data := make([]gin.H, 0, len(hits))
		for _, h := range hits {
			data = append(data, gin.H{
				"message": publicMessage(h.Message),
				"channel": publicChannel(h.Channel),
				"snippet": h.Snippet,
			})
		}

		c.JSON(200, gin.H{
			"data":        data,
			"has_more":    hasMore,
			"next_before": nextBefore,
		})
	}
}

// publicChannel renders a bare store.Channel with string-serialized ids, matching the
// rest of the API's id-as-string convention (store.Channel's own json tags use raw
// int64, which would otherwise leak numeric ids here).
// resolveChannelID resolves a channel ID or a slug:slug string.
func resolveChannelID(ctx context.Context, s *store.Store, param string) (int64, error) {
	if id, err := strconv.ParseInt(param, 10, 64); err == nil {
		return id, nil
	}
	if strings.HasPrefix(param, "slug:") {
		slug := param[5:]
		ch, err := s.GetChannelBySlug(ctx, slug)
		if err != nil {
			return 0, err
		}
		return ch.ID, nil
	}
	ch, err := s.GetChannelBySlug(ctx, param)
	if err != nil {
		return 0, err
	}
	return ch.ID, nil
}
