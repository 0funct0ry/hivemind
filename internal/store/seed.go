package store

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"sort"
	"time"
)

var hourWeights = []int{
	1,  // 00:00
	1,  // 01:00
	1,  // 02:00
	1,  // 03:00
	1,  // 04:00
	2,  // 05:00
	4,  // 06:00
	7,  // 07:00
	10, // 08:00
	15, // 09:00
	20, // 10:00
	22, // 11:00
	18, // 12:00
	22, // 13:00
	24, // 14:00
	25, // 15:00
	23, // 16:00
	17, // 17:00
	12, // 18:00
	8,  // 19:00
	6,  // 20:00
	5,  // 21:00
	3,  // 22:00
	2,  // 23:00
}

var sampleBodies = []string{
	"Hey everyone! Welcome to the channel.",
	"Did we deploy the hotfix to production yet?",
	"I'm seeing some latency spikes on the database read replica.",
	"Let's schedule a meeting to discuss the product roadmap.",
	"Looks good to me, merging the pull request now.",
	"Is anyone else experiencing issues with the staging environment?",
	"We need to update the documentation for the new API endpoints.",
	"Great job on finishing the milestone ahead of schedule!",
	"Can someone review my PR when they get a chance?",
	"Don't forget to submit your weekly status reports.",
	"I'll be out of office tomorrow afternoon.",
	"Who is working on the checkout service refactoring?",
	"The design mockup looks clean, love the dark mode interface.",
	"Let's move this conversation to a separate thread.",
	"We should write more unit tests for the message delivery logic.",
	"Any thoughts on using SQLite WAL mode for better concurrency?",
	"The server logs show a lot of unique constraint violations on sessions.",
	"Please double check the API payload validation constraints.",
	"Are we still on track for the release next Tuesday?",
	"Coffee break? Meet in the cafeteria in 5 minutes.",
}

// Seed populates the database with realistic test data.
func (s *Store) Seed(ctx context.Context, numUsers, numChannels, numMessages int) error {
	if numUsers < 1 {
		numUsers = 1
	}
	if numChannels < 1 {
		numChannels = 1
	}
	if numMessages < 1 {
		numMessages = 1
	}

	// 1. Generate users
	// Hash for "password" pre-calculated to speed up seeding
	const dummyPassHash = "$2a$10$wR.O7F2.B66F5y6/qf/cnuUq/wz9F0Lz2nve1mfe57L54t0d3cI5W"

	// Sum diurnal weights
	totalWeight := 0
	for _, w := range hourWeights {
		totalWeight += w
	}

	rng := rand.New(rand.NewSource(12345)) // Deterministic seed for reproducible fixture data

	return s.Tx(ctx, func(tx *sql.Tx) error {
		var userIDs []int64
		for i := 0; i < numUsers; i++ {
			username := fmt.Sprintf("user%d", i+1)
			email := fmt.Sprintf("user%d@example.com", i+1)
			displayName := fmt.Sprintf("User %d", i+1)
			role := "member"
			if i == 0 {
				role = "admin"
			}

			// Derive stable avatar color
			avatarColor := AvatarColor(username)
			now := time.Now().UnixMilli()

			res, err := tx.ExecContext(ctx, `
				INSERT INTO users (username, email, display_name, password_hash, avatar_color, role, is_bot, status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, 0, 'active', ?, ?)`,
				username, email, displayName, dummyPassHash, avatarColor, role, now, now)
			if err != nil {
				return fmt.Errorf("seed user %s: %w", username, err)
			}
			uid, _ := res.LastInsertId()
			userIDs = append(userIDs, uid)
		}

		// 2. Generate channels
		var channelIDs []int64
		for i := 0; i < numChannels; i++ {
			slug := fmt.Sprintf("channel-%d", i+1)
			name := fmt.Sprintf("Channel %d", i+1)
			topic := fmt.Sprintf("Discussion topic for channel %d", i+1)
			now := time.Now().UnixMilli()

			res, err := tx.ExecContext(ctx, `
				INSERT INTO channels (kind, slug, name, topic, created_by, created_at, updated_at)
				VALUES ('public', ?, ?, ?, ?, ?, ?)`,
				slug, name, topic, userIDs[0], now, now)
			if err != nil {
				return fmt.Errorf("seed channel %s: %w", slug, err)
			}
			cid, _ := res.LastInsertId()
			channelIDs = append(channelIDs, cid)

			// Join all users to all public channels
			for _, uid := range userIDs {
				_, err = tx.ExecContext(ctx, `
					INSERT INTO channel_members (channel_id, user_id, role, joined_at)
					VALUES (?, ?, 'member', ?)`,
					cid, uid, now)
				if err != nil {
					return fmt.Errorf("join user %d to channel %d: %w", uid, cid, err)
				}
			}
		}

		// 3. Generate sorted timestamps over 30 days with diurnal pattern
		nowTime := time.Now()
		timestamps := make([]int64, numMessages)
		for i := 0; i < numMessages; i++ {
			// Pick a day in the last 30 days
			daysAgo := rng.Intn(30)
			
			// Pick hour of the day using diurnal weights
			val := rng.Intn(totalWeight)
			hour := 0
			for h, w := range hourWeights {
				val -= w
				if val < 0 {
					hour = h
					break
				}
			}

			// Random minute/second
			minute := rng.Intn(60)
			second := rng.Intn(60)

			// Construct timestamp
			t := nowTime.AddDate(0, 0, -daysAgo)
			// Align to the selected hour/minute/second
			msgTime := time.Date(t.Year(), t.Month(), t.Day(), hour, minute, second, 0, t.Location())
			timestamps[i] = msgTime.UnixMilli()
		}

		// Sort timestamps ascending to preserve chronological ordering matching ID sequence
		sort.Slice(timestamps, func(i, j int) bool {
			return timestamps[i] < timestamps[j]
		})

		// 4. Insert messages
		for i := 0; i < numMessages; i++ {
			cid := channelIDs[rng.Intn(len(channelIDs))]
			uid := userIDs[rng.Intn(len(userIDs))]
			body := sampleBodies[rng.Intn(len(sampleBodies))]
			ts := timestamps[i]

			res, err := tx.ExecContext(ctx, `
				INSERT INTO messages (channel_id, user_id, body, created_at)
				VALUES (?, ?, ?, ?)`,
				cid, uid, body, ts)
			if err != nil {
				return fmt.Errorf("seed message %d: %w", i, err)
			}

			msgID, _ := res.LastInsertId()

			// Update channel metadata for last message
			_, err = tx.ExecContext(ctx, `
				UPDATE channels
				SET last_message_id = ?, updated_at = ?
				WHERE id = ?`,
				msgID, ts, cid)
			if err != nil {
				return fmt.Errorf("update last message id for channel %d: %w", cid, err)
			}
		}

		return nil
	})
}
