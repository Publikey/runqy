package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/spf13/cobra"
)

// workerCmd represents the worker command
var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Worker management commands",
	Long: `Worker management commands for listing and inspecting runqy workers.

Workers are external processes that connect to runqy server and process tasks.
They register themselves via /worker/register and send heartbeats to Redis.

Examples:
  runqy worker list
  runqy worker info worker-abc123

Remote mode:
  runqy --server https://runqy.example.com -k API_KEY worker list`,
}

// workerListCmd lists all workers
var workerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered workers",
	Long: `List all workers registered in Redis (from asynq:workers:* keys).

Shows worker ID, status, queues, last heartbeat, and staleness.`,
	RunE: runWorkerList,
}

// workerInfoCmd shows detailed info about a worker
var workerInfoCmd = &cobra.Command{
	Use:   "info <worker_id>",
	Short: "Show detailed worker information",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkerInfo,
}

func init() {
	rootCmd.AddCommand(workerCmd)
	workerCmd.AddCommand(workerListCmd)
	workerCmd.AddCommand(workerInfoCmd)
}

const keyWorkerPattern = "asynq:workers:*"

func runWorkerList(cmd *cobra.Command, args []string) error {
	// Remote mode: use API client
	if IsRemoteMode() {
		return runWorkerListRemote()
	}

	// Local mode: direct Redis access
	redisAddr, err := models.BuildRedisConns(GetConfig())
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	defer redisAddr.RDB.Close()

	ctx := context.Background()

	// Scan for all worker keys
	var workerKeys []string
	iter := redisAddr.RDB.Scan(ctx, 0, keyWorkerPattern, 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		if strings.Count(key, ":") >= 2 {
			workerKeys = append(workerKeys, key)
		}
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan workers: %w", err)
	}

	if len(workerKeys) == 0 {
		fmt.Println("No workers found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "WORKER_ID\tSTATUS\tQUEUES\tCONCURRENCY\tLAST_BEAT\tSTALE")

	now := time.Now().Unix()

	for _, workerKey := range workerKeys {
		// Check if it's a HASH type
		keyType, err := redisAddr.RDB.Type(ctx, workerKey).Result()
		if err != nil || keyType != "hash" {
			continue
		}

		data, err := redisAddr.RDB.HGetAll(ctx, workerKey).Result()
		if err != nil || len(data) == 0 {
			continue
		}

		workerID := strings.TrimPrefix(workerKey, "asynq:workers:")
		status := data["status"]
		queues := data["queues"]
		concurrency := data["concurrency"]

		lastBeatStr := data["last_beat"]
		lastBeat, _ := strconv.ParseInt(lastBeatStr, 10, 64)
		isStale := (now - lastBeat) > 30

		staleStr := "no"
		if isStale {
			staleStr = "yes"
		}

		lastBeatAgo := ""
		if lastBeat > 0 {
			ago := time.Since(time.Unix(lastBeat, 0))
			lastBeatAgo = formatDuration(ago)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			workerID,
			status,
			truncate(queueworker.FormatWorkerQueues(queues), 30),
			concurrency,
			lastBeatAgo,
			staleStr,
		)
	}

	w.Flush()
	return nil
}

func runWorkerListRemote() error {
	apiClient := NewAPIClient()

	workers, err := apiClient.ListWorkers()
	if err != nil {
		return err
	}

	if len(workers) == 0 {
		fmt.Println("No workers found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "WORKER_ID\tSTATUS\tQUEUES\tCONCURRENCY\tLAST_BEAT\tSTALE")

	now := time.Now().Unix()

	for _, worker := range workers {
		staleStr := "no"
		if worker.IsStale {
			staleStr = "yes"
		}

		lastBeatAgo := ""
		if worker.LastBeat > 0 {
			ago := time.Since(time.Unix(worker.LastBeat, 0))
			// If remote, use the IsStale field since local time might differ
			if now-worker.LastBeat > 30 {
				staleStr = "yes"
			}
			lastBeatAgo = formatDuration(ago)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			worker.WorkerID,
			worker.Status,
			truncate(queueworker.FormatWorkerQueues(worker.Queues), 30),
			worker.Concurrency,
			lastBeatAgo,
			staleStr,
		)
	}

	w.Flush()
	return nil
}

func runWorkerInfo(cmd *cobra.Command, args []string) error {
	if err := validateArgs(args, "worker ID"); err != nil {
		return err
	}
	workerID := args[0]

	// Remote mode: use API client
	if IsRemoteMode() {
		return runWorkerInfoRemote(workerID)
	}

	// Local mode: direct Redis access
	redisAddr, err := models.BuildRedisConns(GetConfig())
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	defer redisAddr.RDB.Close()

	ctx := context.Background()
	workerKey := "asynq:workers:" + workerID

	data, err := redisAddr.RDB.HGetAll(ctx, workerKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get worker info: %w", err)
	}

	if len(data) == 0 {
		return fmt.Errorf("worker '%s' not found", workerID)
	}

	now := time.Now().Unix()

	fmt.Printf("Worker ID:   %s\n", workerID)
	fmt.Printf("Status:      %s\n", data["status"])
	fmt.Printf("Queues:      %s\n", queueworker.FormatWorkerQueues(data["queues"]))
	fmt.Printf("Concurrency: %s\n", data["concurrency"])

	if startedAt, err := strconv.ParseInt(data["started_at"], 10, 64); err == nil && startedAt > 0 {
		fmt.Printf("Started At:  %s\n", time.Unix(startedAt, 0).Format("2006-01-02 15:04:05"))
	}

	if lastBeat, err := strconv.ParseInt(data["last_beat"], 10, 64); err == nil && lastBeat > 0 {
		fmt.Printf("Last Beat:   %s (%s ago)\n",
			time.Unix(lastBeat, 0).Format("2006-01-02 15:04:05"),
			formatDuration(time.Since(time.Unix(lastBeat, 0))),
		)
		isStale := (now - lastBeat) > 30
		if isStale {
			fmt.Println("WARNING: Worker is STALE (no heartbeat for >30s)")
		}
	}

	// Print any additional fields
	fmt.Println()
	fmt.Println("All fields:")
	for k, v := range data {
		fmt.Printf("  %s: %s\n", k, v)
	}

	return nil
}

func runWorkerInfoRemote(workerID string) error {
	apiClient := NewAPIClient()

	worker, err := apiClient.GetWorker(workerID)
	if err != nil {
		return err
	}

	now := time.Now().Unix()

	fmt.Printf("Worker ID:   %s\n", worker.WorkerID)
	fmt.Printf("Status:      %s\n", worker.Status)
	fmt.Printf("Queues:      %s\n", queueworker.FormatWorkerQueues(worker.Queues))
	fmt.Printf("Concurrency: %d\n", worker.Concurrency)

	if worker.StartedAt > 0 {
		fmt.Printf("Started At:  %s\n", time.Unix(worker.StartedAt, 0).Format("2006-01-02 15:04:05"))
	}

	if worker.LastBeat > 0 {
		fmt.Printf("Last Beat:   %s (%s ago)\n",
			time.Unix(worker.LastBeat, 0).Format("2006-01-02 15:04:05"),
			formatDuration(time.Since(time.Unix(worker.LastBeat, 0))),
		)
		isStale := (now - worker.LastBeat) > 30
		if isStale || worker.IsStale {
			fmt.Println("WARNING: Worker is STALE (no heartbeat for >30s)")
		}
	}

	return nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
