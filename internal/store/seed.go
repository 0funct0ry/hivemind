package store

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// seedBcryptCost matches internal/auth.bcryptCost; duplicated here since
// internal/auth imports internal/store and would create an import cycle.
const seedBcryptCost = 12

// seedUsers lists the realistic fixture identities used by Seed, in order. The
// first entry is always seeded as the admin user.
var seedUsers = []struct {
	username string
	email    string
}{
	{"bruce", "bruce@hivemind.com"},
	{"oliver", "oliver@hivemind.com"},
	{"hugo", "hugo@hivemind.com"},
	{"felix", "felix@hivemind.com"},
	{"arthur", "arthur@hivemind.com"},
	{"leo", "leo@hivemind.com"},
	{"oscar", "oscar@hivemind.com"},
	{"louis", "louis@hivemind.com"},
	{"edgar", "edgar@hivemind.com"},
	{"henry", "henry@hivemind.com"},
	{"alain", "alain@hivemind.com"},
	{"emil", "emil@hivemind.com"},
	{"otto", "otto@hivemind.com"},
	{"hans", "hans@hivemind.com"},
	{"erik", "erik@hivemind.com"},
	{"niels", "niels@hivemind.com"},
	{"lars", "lars@hivemind.com"},
	{"ivan", "ivan@hivemind.com"},
	{"marc", "marc@hivemind.com"},
	{"pierre", "pierre@hivemind.com"},
	{"clara", "clara@hivemind.com"},
	{"elise", "elise@hivemind.com"},
	{"sophie", "sophie@hivemind.com"},
	{"anna", "anna@hivemind.com"},
	{"emma", "emma@hivemind.com"},
	{"ines", "ines@hivemind.com"},
	{"lena", "lena@hivemind.com"},
	{"sara", "sara@hivemind.com"},
	{"mila", "mila@hivemind.com"},
	{"nina", "nina@hivemind.com"},
	{"eva", "eva@hivemind.com"},
	// NB: the source list repeats "clara" (#21 and #32) — skipped here since
	// usernames must be unique.
	{"freya", "freya@hivemind.com"},
	{"iris", "iris@hivemind.com"},
	{"alice", "alice@hivemind.com"},
	{"greta", "greta@hivemind.com"},
	{"elsa", "elsa@hivemind.com"},
	{"helga", "helga@hivemind.com"},
	{"astrid", "astrid@hivemind.com"},
	{"maja", "maja@hivemind.com"},
}

// DefaultSeedPassword is applied to every seeded user when the caller does
// not specify one explicitly (e.g. via the `seed --password` flag).
const DefaultSeedPassword = "s3cr3t@1234"

// channelNames is the pool of realistic channel slugs/names Seed draws from.
var channelNames = []string{
	"general",
	"announcements",
	"random",
	"watercooler",
	"social",
	"off-topic",
	"welcome",
	"introductions",
	"team",
	"leadership",
	"management",
	"engineering",
	"software",
	"hardware",
	"firmware",
	"embedded",
	"frontend",
	"backend",
	"fullstack",
	"platform",
	"architecture",
	"devops",
	"sre",
	"infrastructure",
	"cloud",
	"security",
	"qa",
	"testing",
	"automation",
	"performance",
	"data",
	"analytics",
	"ai",
	"machine-learning",
	"dev-tools",
	"open-source",
	"api",
	"microservices",
	"distributed-systems",
	"databases",
	"mobile",
	"ios",
	"android",
	"web",
	"ux",
	"ui",
	"design",
	"product",
	"product-management",
	"product-roadmap",
	"requirements",
	"customer-success",
	"support",
	"sales",
	"marketing",
	"finance",
	"hr",
	"recruiting",
	"legal",
	"procurement",
	"operations",
	"facilities",
	"projects",
	"program-management",
	"project-alpha",
	"project-beta",
	"project-gamma",
	"release",
	"releases",
	"release-management",
	"deployment",
	"production",
	"staging",
	"incident-response",
	"incidents",
	"on-call",
	"maintenance",
	"outages",
	"postmortems",
	"change-management",
	"ci-cd",
	"builds",
	"code-review",
	"pull-requests",
	"git",
	"documentation",
	"technical-writing",
	"architecture-review",
	"design-review",
	"code-quality",
	"refactoring",
	"tech-debt",
	"performance-testing",
	"load-testing",
	"security-review",
	"vulnerability-management",
	"compliance",
	"privacy",
	"identity",
	"authentication",
	"authorization",
	"networking",
	"kubernetes",
	"docker",
	"linux",
	"windows",
	"macos",
	"golang",
	"java",
	"rust",
	"python",
	"cpp",
	"javascript",
	"typescript",
	"react",
	"embedded-linux",
	"rtos",
	"firmware-dev",
	"device-drivers",
	"board-support",
	"hardware-design",
	"pcb-design",
	"electronics",
	"electrical",
	"power-electronics",
	"analog",
	"digital",
	"mixed-signal",
	"asic",
	"fpga",
	"vlsi",
	"semiconductor",
	"chip-design",
	"rtl",
	"verification",
	"system-verilog",
	"verilog",
	"uvm",
	"eda",
	"physical-design",
	"place-and-route",
	"floorplanning",
	"timing",
	"sta",
	"clock-design",
	"power-analysis",
	"signal-integrity",
	"design-for-test",
	"dft",
	"scan",
	"memory-design",
	"ip-design",
	"soc",
	"silicon",
	"tapeout",
	"foundry",
	"wafer",
	"packaging",
	"semiconductor-testing",
	"lab",
	"hardware-validation",
	"prototype",
	"manufacturing",
	"production-engineering",
	"quality",
	"quality-control",
	"supply-chain",
	"logistics",
	"vendors",
	"suppliers",
	"inventory",
	"procurement-team",
	"automotive",
	"vehicle-platform",
	"vehicle-engineering",
	"automotive-software",
	"adas",
	"autonomous-driving",
	"connected-car",
	"infotainment",
	"telematics",
	"body-electronics",
	"powertrain",
	"electric-vehicles",
	"ev",
	"battery",
	"battery-management",
	"charging",
	"motor-control",
	"vehicle-controls",
	"chassis",
	"braking",
	"steering",
	"thermal-management",
	"vehicle-safety",
	"functional-safety",
	"iso26262",
	"autosar",
	"can-bus",
	"lin-bus",
	"automotive-ethernet",
	"vehicle-testing",
	"test-track",
	"homologation",
	"recalls",
	"field-issues",
	"customer-feedback",
	"innovation",
	"research",
	"r-and-d",
	"hackathon",
	"learning",
	"training",
	"career",
	"books",
	"conference",
	"events",
	"community",
	"chit-chat",
	"coffee",
	"lunch",
	"travel",
	"photos",
	"pets",
	"weekend",
}

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
	"Has anyone figured out why the CI pipeline is taking almost 20 minutes?",
	"I pushed a small optimization to the caching layer. Would appreciate a review.",
	"The new feature flag is enabled in staging, please give it a try.",
	"Can we avoid adding another dependency for something the standard library already supports?",
	"The integration tests are flaky again, especially the payment timeout scenario.",
	"I've added request IDs to the logs so we can trace calls across services.",
	"Please don't merge this until the database migration has been tested against a realistic dataset.",
	"The API is returning 500s when the downstream service sends an empty response.",
	"I'll investigate the memory growth we're seeing in the worker process.",
	"Does anyone have an example of the expected webhook payload?",
	"The production dashboard looks healthy again after the restart.",
	"We should probably add a circuit breaker around that third-party API.",
	"Can we make this configuration optional instead of requiring it for every environment?",
	"I found the bug. We were comparing timestamps in local time instead of UTC.",
	"The Docker image grew by almost 400 MB after the last change.",
	"Let's add a benchmark before we optimize this further.",
	"The staging database hasn't been refreshed in a while, so some tests may be misleading.",
	"I've opened an issue for the race condition we found in the background worker.",
	"Would a queue make more sense here than processing everything synchronously?",
	"The frontend build is failing only on Node 22. Works fine on the current CI image.",
	"Can someone check whether the Redis connection pool is being closed correctly?",
	"I've added pagination to the endpoint because the response was getting too large.",
	"The new error messages should be more useful to API consumers than just returning 'internal error'.",
	"Let's keep the migration backward compatible so we can roll back safely.",
	"I think we have a missing index on the transactions table.",
	"The smoke tests passed, so we're good to proceed with the deployment.",
	"Please add a test for the empty result case before merging.",
	"Anyone know why the Kubernetes pod keeps getting OOMKilled?",
	"I'll pair with you on the authentication issue after lunch.",
	"The API contract has changed, so the generated client needs to be regenerated.",
	"We should document the retry behavior before consumers start relying on it.",
	"I've added structured logging instead of concatenating strings in the service.",
	"The new dashboard query is surprisingly fast even with ten million rows.",
	"Let's avoid putting business logic directly into the HTTP handlers.",
	"I noticed a goroutine leak when the downstream connection times out.",
	"Can we get a second opinion on this concurrency approach?",
	"The deployment rollback completed successfully and traffic is back to normal.",
	"I've attached the benchmark results to the ticket.",
	"The acceptance criteria don't mention what should happen when the user retries the request.",
	"We should probably make this operation idempotent.",
	"Is the test environment currently pointing to the new version of the authentication service?",
	"I've updated the OpenAPI specification with the new endpoint and response schema.",
	"The frontend needs a loading state here; otherwise it looks like the button isn't working.",
	"Can we add a confirmation before allowing users to delete all their data?",
	"The database migration works locally but fails on a clean database.",
	"Looks like the issue is caused by a missing environment variable in the deployment manifest.",
	"I've added tracing around the slow database queries so we can see where the time is going.",
	"Let's not increase the timeout globally just to accommodate this one slow endpoint.",
	"The nightly backup completed successfully.",
	"Does anyone know if this endpoint is still being used by the mobile application?",
	"I'll check the access logs before we remove the deprecated API.",
	"The PR is getting large. Can we split it into smaller changes?",
	"We should add contract tests between the order service and the payment service.",
	"The new retry policy accidentally caused a request storm during the outage.",
	"Please be careful changing shared configuration; several services depend on it.",
	"I've reproduced the issue locally and should have a fix shortly.",
	"Can we add metrics for successful, failed, and retried requests separately?",
	"The feature works, but I think the naming could be clearer before we merge it.",
	"Let's record the architectural decision so we don't have the same discussion again in three months.",
	"The service is healthy, but response times are still higher than our SLO.",
	"I've removed an unused endpoint and updated the API documentation.",
	"Could someone verify the migration on PostgreSQL 15 as well as 16?",
	"The consumer is falling behind because messages are being processed sequentially.",
	"I think we can safely batch these database writes to reduce round trips.",
	"The new UI looks great on desktop, but the table is difficult to use on smaller screens.",
	"Please don't log access tokens or other sensitive request headers.",
	"We need a proper local development setup so new developers don't have to configure ten services manually.",
	"The release notes are ready for review.",
	"Has the security scan finished running on the latest container image?",
	"I've added graceful shutdown handling so the service can finish in-flight requests.",
	"The health check is currently testing the database, which makes the service look unhealthy during a database outage.",
	"Let's distinguish between liveness and readiness checks here.",
	"The generated test data doesn't cover enough edge cases around failed transactions.",
	"I found an old TODO that has been there since 2021. I think it's finally time to address it.",
	"Can we add a timeout to this outbound HTTP request? It currently has none.",
	"The dependency upgrade fixed the vulnerability without requiring any code changes.",
	"I've added a command to inspect the current configuration from the CLI.",
	"The worker should acknowledge the message only after the database transaction commits.",
	"Let's make the default configuration useful for local development without requiring a bunch of flags.",
	"The API should return 409 here instead of 400 because this is a resource conflict.",
	"I've added a regression test for the bug reported yesterday.",
	"Can we run the load test with a traffic pattern closer to production?",
	"The deployment is blocked because the container registry is reporting an authentication error.",
	"I think we should introduce a repository interface here to keep the domain independent of the database.",
	"The frontend error boundary caught the exception, but we should still fix the underlying issue.",
	"Please check the clock skew handling before we merge the token validation changes.",
	"The service currently starts even when its required configuration is missing, which makes failures harder to diagnose.",
	"I'll clean up the temporary debugging code before opening the PR.",
	"Has anyone tried running the complete test suite with the race detector enabled?",
	"The migration adds a unique constraint, so we need to clean up duplicate records first.",
	"We should expose a metric for queue depth before increasing the number of workers.",
	"The API documentation is out of sync with the actual response returned by the service.",
	"I've created a small reproduction case that demonstrates the concurrency bug.",
	"Let's avoid premature abstraction until we have a second use case.",
	"The cache invalidation logic is more complicated than the original feature.",
	"I'll monitor the deployment for the next 30 minutes and keep an eye on error rates.",
}

// Seed populates the database with realistic test data. Every seeded user
// gets password as their login password (all users share the same password
// since this is fixture data, not a real deployment).
func (s *Store) Seed(ctx context.Context, numUsers, numChannels, numMessages int, password string) error {
	if numUsers < 1 {
		numUsers = 1
	}
	if numUsers > len(seedUsers) {
		numUsers = len(seedUsers)
	}
	if numChannels < 1 {
		numChannels = 1
	}
	if numChannels > len(channelNames) {
		numChannels = len(channelNames)
	}
	if numMessages < 1 {
		numMessages = 1
	}
	if password == "" {
		password = DefaultSeedPassword
	}

	passHashBytes, err := bcrypt.GenerateFromPassword([]byte(password), seedBcryptCost)
	if err != nil {
		return fmt.Errorf("hash seed password: %w", err)
	}
	passHash := string(passHashBytes)

	// Sum diurnal weights
	totalWeight := 0
	for _, w := range hourWeights {
		totalWeight += w
	}

	rng := rand.New(rand.NewSource(12345)) // Deterministic seed for reproducible fixture data

	return s.Tx(ctx, func(tx *sql.Tx) error {
		var userIDs []int64
		for i := 0; i < numUsers; i++ {
			username := seedUsers[i].username
			email := seedUsers[i].email
			displayName := strings.ToUpper(username[:1]) + username[1:]
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
				username, email, displayName, passHash, avatarColor, role, now, now)
			if err != nil {
				return fmt.Errorf("seed user %s: %w", username, err)
			}
			uid, _ := res.LastInsertId()
			userIDs = append(userIDs, uid)
		}

		// 2. Generate channels, drawing distinct names from the channelNames pool
		channelOrder := rng.Perm(len(channelNames))
		var channelIDs []int64
		for i := 0; i < numChannels; i++ {
			slug := channelNames[channelOrder[i]]
			name := slug
			topic := fmt.Sprintf("Discussion topic for #%s", slug)
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
