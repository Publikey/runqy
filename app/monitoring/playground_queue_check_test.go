package monitoring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"

	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/third_party/asynq"
)

func newPlaygroundTestStore(t *testing.T, rdb *redis.Client) *queueworker.Store {
	t.Helper()
	db, err := sqlx.Connect("sqlite", filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
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
		Name: "video.high", Priority: 5,
		Deployment: &queueworker.DeploymentConfig{GitURL: "https://example.com/r.git", StartupCmd: "python main.py"},
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

// The playground must refuse a queue no worker can consume, like POST /queue/add.
func TestPlaygroundRejectsUnknownQueue(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	handler := newPlaygroundEnqueueHandlerFunc(asynq.NewClientFromRedisClient(rdb), rdb, newPlaygroundTestStore(t, rdb))

	for queue, want := range map[string]int{"video.high": http.StatusOK, "video.low": http.StatusNotFound, "other": http.StatusNotFound} {
		req := httptest.NewRequest("POST", "/api/playground/enqueue", strings.NewReader(`{"queue":"`+queue+`","payload":{}}`))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d (%s)", queue, rec.Code, want, rec.Body.String())
		}
	}
	if n, _ := rdb.LLen(context.Background(), "asynq:{video.low}:pending").Result(); n != 0 {
		t.Fatalf("task enqueued on an unknown sub-queue")
	}
}
