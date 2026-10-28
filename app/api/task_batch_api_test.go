package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"

	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/third_party/asynq"
)

// Batch-enqueued tasks must carry their lifecycle limits from the moment they are pending
// (previously stamped by a pipeline flushed every 100 jobs, after the tasks were consumable).
func TestAddTaskBatchStampsLimitsAtEnqueue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	db, err := sqlx.Connect("sqlite", filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE queues (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT UNIQUE NOT NULL, provider TEXT DEFAULT '',
			deployment TEXT, input_schema TEXT, output_schema TEXT, description TEXT DEFAULT '',
			enabled INTEGER DEFAULT 1, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE sub_queues (
			id INTEGER PRIMARY KEY AUTOINCREMENT, queue_id INTEGER NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
			name TEXT NOT NULL, priority INTEGER NOT NULL DEFAULT 1, enabled INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(queue_id, name));`); err != nil {
		t.Fatal(err)
	}
	store := queueworker.NewStore(db, rdb)
	if err := store.Save(context.Background(), &queueworker.QueueConfig{
		Name: "video.low", Priority: 1,
		Deployment: &queueworker.DeploymentConfig{
			GitURL: "https://example.com/r.git", StartupCmd: "python main.py",
			Limits: &queueworker.LimitsConfig{PendingTimeout: "5m"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("client", asynq.NewClientFromRedisClient(rdb))
		c.Set("rdb", rdb)
	})
	r.POST("/batch", AddTaskBatch(t.TempDir(), store))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/batch",
		strings.NewReader(`{"queue":"video.low","timeout":60,"jobs":[{"a":1},{"a":2}]}`)))
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	ctx := context.Background()
	ids, _ := rdb.LRange(ctx, "asynq:{video.low}:pending", 0, -1).Result()
	if len(ids) != 2 {
		t.Fatalf("pending = %v", ids)
	}
	for _, id := range ids {
		h, _ := rdb.HGetAll(ctx, "asynq:{video.low}:t:"+id).Result()
		if h["pending_timeout"] != "300" || h["active_timeout"] != "60" {
			t.Errorf("task %s limits = pending %q / active %q, want 300 / 60", id, h["pending_timeout"], h["active_timeout"])
		}
	}
}
