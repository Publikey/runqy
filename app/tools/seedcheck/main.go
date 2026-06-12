// Throwaway validation: checks that tasks written by scripts/seed_dummy_data.py
// decode correctly through the asynq Inspector (same read path as the dashboard).
package main

import (
	"fmt"
	"os"

	"github.com/Publikey/runqy/third_party/asynq"
)

func main() {
	insp := asynq.NewInspector(asynq.RedisClientOpt{
		Addr:     "localhost:6379",
		Password: os.Getenv("SEEDCHECK_REDIS_PASSWORD"),
	})
	defer insp.Close()

	queues, err := insp.Queues()
	if err != nil {
		fmt.Println("Queues() error:", err)
		os.Exit(1)
	}
	fmt.Println("queues:", queues)

	for _, q := range queues {
		info, err := insp.GetQueueInfo(q)
		if err != nil {
			fmt.Printf("%s: GetQueueInfo error: %v\n", q, err)
			continue
		}
		fmt.Printf("%s: pending=%d active=%d scheduled=%d retry=%d completed=%d archived=%d paused=%v latency=%s\n",
			q, info.Pending, info.Active, info.Scheduled, info.Retry, info.Completed, info.Archived, info.Paused, info.Latency)
	}

	// Decode actual task messages from each state (this exercises protobuf decoding).
	for _, list := range []struct {
		name string
		fn   func() ([]*asynq.TaskInfo, error)
	}{
		{"pending(inference.high)", func() ([]*asynq.TaskInfo, error) { return insp.ListPendingTasks("inference.high", asynq.PageSize(2)) }},
		{"retry(inference.high)", func() ([]*asynq.TaskInfo, error) { return insp.ListRetryTasks("inference.high", asynq.PageSize(2)) }},
		{"completed(inference.high)", func() ([]*asynq.TaskInfo, error) { return insp.ListCompletedTasks("inference.high", asynq.PageSize(2)) }},
		{"archived(video-transcode.default)", func() ([]*asynq.TaskInfo, error) { return insp.ListArchivedTasks("video-transcode.default", asynq.PageSize(2)) }},
	} {
		tasks, err := list.fn()
		if err != nil {
			fmt.Printf("%s: ERROR %v\n", list.name, err)
			os.Exit(1)
		}
		for _, t := range tasks {
			fmt.Printf("%s: id=%s retried=%d/%d completed_at=%d failed_at=%d err=%.40q\n",
				list.name, t.ID, t.Retried, t.MaxRetry, t.CompletedAt.Unix(), t.LastFailedAt.Unix(), t.LastErr)
		}
	}
	fmt.Println("OK")
}
