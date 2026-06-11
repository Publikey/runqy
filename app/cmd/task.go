package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/Publikey/runqy/client"
	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/spf13/cobra"
)

var (
	taskQueue   string
	taskPayload string
	taskTimeout int64
	taskState   string
	taskLimit   int
)

// taskCmd represents the task command
var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task management commands",
	Long: `Task management commands for enqueueing, listing, and managing tasks.

Examples:
  runqy task enqueue --queue inference_high --payload '{"msg":"hello"}'
  runqy task list inference_high
  runqy task get inference_high abc123
  runqy task cancel abc123

Remote mode:
  runqy --server https://runqy.example.com -k API_KEY task enqueue -q inference_high -p '{"msg":"hello"}'`,
}

// taskEnqueueCmd enqueues a new task
var taskEnqueueCmd = &cobra.Command{
	Use:   "enqueue",
	Short: "Enqueue a new task",
	Long: `Enqueue a new task to the specified queue with a JSON payload.

Examples:
  runqy task enqueue --queue inference_high --payload '{"msg":"hello"}'
  runqy task enqueue -q inference_high -p '{"key":"value"}' --timeout 300`,
	RunE: runTaskEnqueue,
}

// taskListCmd lists tasks in a queue
var taskListCmd = &cobra.Command{
	Use:   "list <queue>",
	Short: "List tasks in a queue",
	Long: `List tasks in the specified queue. Use --state to filter by task state.

States: pending, active, scheduled, retry, archived, completed

Examples:
  runqy task list inference_high
  runqy task list inference_high --state pending
  runqy task list inference_high --state active --limit 20`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskList,
}

// taskGetCmd gets info about a specific task
var taskGetCmd = &cobra.Command{
	Use:   "get <queue> <task_id>",
	Short: "Get task details",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskGet,
}

// taskCancelCmd cancels a task
var taskCancelCmd = &cobra.Command{
	Use:   "cancel <task_id>",
	Short: "Cancel a running task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskCancel,
}

// taskDeleteCmd deletes a task
var taskDeleteCmd = &cobra.Command{
	Use:   "delete <queue> <task_id>",
	Short: "Delete a task from a queue",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskDelete,
}

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskEnqueueCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
	taskCmd.AddCommand(taskCancelCmd)
	taskCmd.AddCommand(taskDeleteCmd)

	// Enqueue flags
	taskEnqueueCmd.Flags().StringVarP(&taskQueue, "queue", "q", "", "Queue name (required)")
	taskEnqueueCmd.Flags().StringVarP(&taskPayload, "payload", "p", "{}", "JSON payload")
	taskEnqueueCmd.Flags().Int64VarP(&taskTimeout, "timeout", "t", 600, "Task timeout in seconds")
	taskEnqueueCmd.MarkFlagRequired("queue")

	// List flags
	taskListCmd.Flags().StringVar(&taskState, "state", "pending", "Task state (pending, active, scheduled, retry, archived, completed)")
	taskListCmd.Flags().IntVarP(&taskLimit, "limit", "l", 10, "Maximum number of tasks to show")
}

func runTaskEnqueue(cmd *cobra.Command, args []string) error {
	if taskTimeout <= 0 {
		return fmt.Errorf("--timeout must be positive, got %d", taskTimeout)
	}

	// Validate JSON payload
	var payload json.RawMessage
	if err := json.Unmarshal([]byte(taskPayload), &payload); err != nil {
		return fmt.Errorf("invalid JSON payload: %w", err)
	}

	// Remote mode: use API client
	if IsRemoteMode() {
		apiClient := NewAPIClient()
		taskInfo, err := apiClient.EnqueueTask(taskQueue, payload, taskTimeout)
		if err != nil {
			return fmt.Errorf("failed to enqueue task: %w", err)
		}

		fmt.Printf("Task enqueued successfully!\n")
		fmt.Printf("  ID:    %s\n", taskInfo.ID)
		fmt.Printf("  Queue: %s\n", taskInfo.Queue)
		fmt.Printf("  Type:  %s\n", taskInfo.Type)
		return nil
	}

	// Local mode: direct Redis access
	redisAddr, err := models.BuildRedisConns(GetConfig())
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	asynqClient := asynq.NewClient(redisAddr.AsynqOpt)
	defer asynqClient.Close()

	// Local CLI enqueue uses server-default limits (env-driven); the server API path resolves
	// per-queue overrides from the queue config.
	taskInfo, err := client.EnqueueGenericTask(asynqClient, redisAddr.RDB, taskQueue, taskTimeout, payload, queueworker.DefaultLimits())
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	fmt.Printf("Task enqueued successfully!\n")
	fmt.Printf("  ID:    %s\n", taskInfo.ID)
	fmt.Printf("  Queue: %s\n", taskInfo.Queue)
	fmt.Printf("  Type:  %s\n", taskInfo.Type)
	return nil
}

func runTaskList(cmd *cobra.Command, args []string) error {
	if err := validateArgs(args, "queue name"); err != nil {
		return err
	}

	// Validate --state before connecting to Redis
	validStates := map[string]bool{
		"pending": true, "active": true, "scheduled": true,
		"retry": true, "archived": true, "completed": true,
	}
	if !validStates[taskState] {
		return fmt.Errorf("invalid state '%s', valid: pending, active, scheduled, retry, archived, completed", taskState)
	}

	// Validate --limit
	if taskLimit <= 0 {
		return fmt.Errorf("--limit must be positive, got %d", taskLimit)
	}

	queueName := args[0]

	// Remote mode: use API client
	if IsRemoteMode() {
		return runTaskListRemote(queueName)
	}

	// Local mode: direct Redis access
	inspector, err := getInspector()
	if err != nil {
		return err
	}
	defer inspector.Close()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	switch taskState {
	case "pending":
		tasks, err := inspector.ListPendingTasks(queueName, asynq.PageSize(taskLimit))
		if err != nil {
			return fmt.Errorf("failed to list pending tasks: %w", err)
		}
		fmt.Fprintf(w, "ID\tTYPE\tPAYLOAD\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, truncate(string(t.Payload), 50))
		}

	case "active":
		tasks, err := inspector.ListActiveTasks(queueName, asynq.PageSize(taskLimit))
		if err != nil {
			return fmt.Errorf("failed to list active tasks: %w", err)
		}
		fmt.Fprintf(w, "ID\tTYPE\tSTARTED\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, formatTime(t.LastFailedAt))
		}

	case "scheduled":
		tasks, err := inspector.ListScheduledTasks(queueName, asynq.PageSize(taskLimit))
		if err != nil {
			return fmt.Errorf("failed to list scheduled tasks: %w", err)
		}
		fmt.Fprintf(w, "ID\tTYPE\tPROCESS_AT\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, formatTime(t.NextProcessAt))
		}

	case "retry":
		tasks, err := inspector.ListRetryTasks(queueName, asynq.PageSize(taskLimit))
		if err != nil {
			return fmt.Errorf("failed to list retry tasks: %w", err)
		}
		fmt.Fprintf(w, "ID\tTYPE\tRETRIED\tMAX_RETRY\tNEXT_RETRY\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n", t.ID, t.Type, t.Retried, t.MaxRetry, formatTime(t.NextProcessAt))
		}

	case "archived":
		tasks, err := inspector.ListArchivedTasks(queueName, asynq.PageSize(taskLimit))
		if err != nil {
			return fmt.Errorf("failed to list archived tasks: %w", err)
		}
		fmt.Fprintf(w, "ID\tTYPE\tERROR\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, truncate(t.LastErr, 50))
		}

	case "completed":
		tasks, err := inspector.ListCompletedTasks(queueName, asynq.PageSize(taskLimit))
		if err != nil {
			return fmt.Errorf("failed to list completed tasks: %w", err)
		}
		fmt.Fprintf(w, "ID\tTYPE\tCOMPLETED_AT\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, formatTime(t.CompletedAt))
		}

	default:
		return fmt.Errorf("unknown state: %s (valid: pending, active, scheduled, retry, archived, completed)", taskState)
	}

	w.Flush()
	return nil
}

func runTaskListRemote(queueName string) error {
	apiClient := NewAPIClient()

	tasks, err := apiClient.ListTasks(queueName, taskState, taskLimit)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	switch taskState {
	case "pending":
		fmt.Fprintf(w, "ID\tTYPE\tPAYLOAD\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, truncate(string(t.Payload), 50))
		}
	case "active":
		fmt.Fprintf(w, "ID\tTYPE\tSTATE\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, t.State)
		}
	case "scheduled":
		fmt.Fprintf(w, "ID\tTYPE\tPROCESS_AT\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, t.NextProcessAt)
		}
	case "retry":
		fmt.Fprintf(w, "ID\tTYPE\tRETRIED\tMAX_RETRY\tNEXT_RETRY\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n", t.ID, t.Type, t.Retried, t.MaxRetry, t.NextProcessAt)
		}
	case "archived":
		fmt.Fprintf(w, "ID\tTYPE\tERROR\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, truncate(t.LastErr, 50))
		}
	case "completed":
		fmt.Fprintf(w, "ID\tTYPE\tCOMPLETED_AT\n")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Type, t.CompletedAt)
		}
	}

	w.Flush()
	return nil
}

func runTaskGet(cmd *cobra.Command, args []string) error {
	if err := validateArgs(args, "queue name", "task ID"); err != nil {
		return err
	}
	queueName := args[0]
	taskID := args[1]

	// Remote mode: use API client
	if IsRemoteMode() {
		return runTaskGetRemote(queueName, taskID)
	}

	// Local mode: direct Redis access
	inspector, err := getInspector()
	if err != nil {
		return err
	}
	defer inspector.Close()

	info, err := inspector.GetTaskInfo(queueName, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task info: %w", err)
	}

	fmt.Printf("Task ID:     %s\n", info.ID)
	fmt.Printf("Type:        %s\n", info.Type)
	fmt.Printf("Queue:       %s\n", info.Queue)
	fmt.Printf("State:       %s\n", info.State)
	fmt.Printf("Max Retry:   %d\n", info.MaxRetry)
	fmt.Printf("Retried:     %d\n", info.Retried)
	fmt.Printf("Timeout:     %s\n", info.Timeout)
	fmt.Printf("Deadline:    %s\n", formatTime(info.Deadline))
	fmt.Println()
	fmt.Println("Payload:")
	fmt.Println(prettyJSON(info.Payload))

	if info.LastErr != "" {
		fmt.Println()
		fmt.Printf("Last Error: %s\n", info.LastErr)
	}

	if len(info.Result) > 0 {
		fmt.Println()
		fmt.Println("Result:")
		fmt.Println(prettyJSON(info.Result))
	}

	return nil
}

func runTaskGetRemote(queueName, taskID string) error {
	apiClient := NewAPIClient()

	info, err := apiClient.GetTask(queueName, taskID)
	if err != nil {
		return err
	}

	fmt.Printf("Task ID:     %s\n", info.ID)
	fmt.Printf("Type:        %s\n", info.Type)
	fmt.Printf("Queue:       %s\n", info.Queue)
	fmt.Printf("State:       %s\n", info.State)
	fmt.Printf("Max Retry:   %d\n", info.MaxRetry)
	fmt.Printf("Retried:     %d\n", info.Retried)
	fmt.Printf("Timeout:     %s\n", info.Timeout)
	if info.Deadline != "" {
		fmt.Printf("Deadline:    %s\n", info.Deadline)
	}
	fmt.Println()
	fmt.Println("Payload:")
	fmt.Println(prettyJSON(info.Payload))

	if info.LastErr != "" {
		fmt.Println()
		fmt.Printf("Last Error: %s\n", info.LastErr)
	}

	if len(info.Result) > 0 {
		fmt.Println()
		fmt.Println("Result:")
		fmt.Println(prettyJSON(info.Result))
	}

	return nil
}

func runTaskCancel(cmd *cobra.Command, args []string) error {
	if err := validateArgs(args, "task ID"); err != nil {
		return err
	}
	taskID := args[0]

	// Remote mode: use API client
	if IsRemoteMode() {
		apiClient := NewAPIClient()
		if err := apiClient.CancelTask(taskID); err != nil {
			return err
		}
		fmt.Printf("Task '%s' cancelled.\n", taskID)
		return nil
	}

	// Local mode: direct Redis access
	inspector, err := getInspector()
	if err != nil {
		return err
	}
	defer inspector.Close()

	if err := inspector.CancelProcessing(taskID); err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}

	fmt.Printf("Task '%s' cancelled.\n", taskID)
	return nil
}

func runTaskDelete(cmd *cobra.Command, args []string) error {
	if err := validateArgs(args, "queue name", "task ID"); err != nil {
		return err
	}
	queueName := args[0]
	taskID := args[1]

	// Remote mode: use API client
	if IsRemoteMode() {
		apiClient := NewAPIClient()
		if err := apiClient.DeleteTask(queueName, taskID); err != nil {
			return err
		}
		fmt.Printf("Task '%s' deleted from queue '%s'.\n", taskID, queueName)
		return nil
	}

	// Local mode: direct Redis access
	inspector, err := getInspector()
	if err != nil {
		return err
	}
	defer inspector.Close()

	if err := inspector.DeleteTask(queueName, taskID); err != nil {
		return fmt.Errorf("failed to delete task '%s' from queue '%s': %w", taskID, queueName, err)
	}

	fmt.Printf("Task '%s' deleted from queue '%s'.\n", taskID, queueName)
	return nil
}

// Helper functions

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func prettyJSON(data []byte) string {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	pretty, err := json.MarshalIndent(v, "  ", "  ")
	if err != nil {
		return string(data)
	}
	return "  " + string(pretty)
}
