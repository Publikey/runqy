package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Publikey/runqy/control"
	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/spf13/cobra"
)

var (
	controlTarget  string
	controlVersion string
	controlTimeout string
)

var controlCmd = &cobra.Command{
	Use:   "control",
	Short: "Send commands to workers (drain, resume, code update, worker update, config reload)",
	Long: `Send a command to workers over the Redis control channel.

Without --target the command goes to every worker. A target is a worker id (as shown by
'runqy worker list') or a queue name (all workers serving that parent queue).
Delivery is fire-and-forget: check the outcome with 'runqy worker info <id>'. Workers
older than runqy-worker v0.4.0 do not listen and are not counted as receivers.

Examples:
  runqy control drain --target inference          # stop taking tasks on inference
  runqy control resume --target inference
  runqy control code-update --target inference    # git update + deps + restart
  runqy control worker-update --version 0.4.0     # install a release in place
  runqy control config-reload --target inference  # runs the task's @reload

Remote mode:
  runqy --server https://runqy.example.com -k API_KEY control drain --target <worker_id>`,
}

func newControlSubcommand(use, action, short string) *cobra.Command {
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runControl(control.Request{Action: action, Target: controlTarget, Version: controlVersion, Timeout: controlTimeout})
		},
	}
	c.Flags().StringVarP(&controlTarget, "target", "t", "", "Worker id or queue name (default: all workers)")
	c.Flags().StringVar(&controlTimeout, "timeout", "", "How long to wait for in-flight tasks, e.g. 10m (code/worker update default 30m)")
	return c
}

func init() {
	rootCmd.AddCommand(controlCmd)
	controlCmd.AddCommand(newControlSubcommand("drain", control.ActionDrain, "Stop taking new tasks; in-flight tasks finish"))
	controlCmd.AddCommand(newControlSubcommand("resume", control.ActionResume, "Take tasks again after a drain"))
	controlCmd.AddCommand(newControlSubcommand("code-update", control.ActionCodeUpdate, "Update the task code, reinstall dependencies and restart the process"))
	controlCmd.AddCommand(newControlSubcommand("config-reload", control.ActionConfigReload, "Ask the task process to reload its configuration (@reload)"))
	update := newControlSubcommand("worker-update", control.ActionWorkerUpdate, "Install a runqy-worker release and restart in place")
	update.Flags().StringVar(&controlVersion, "version", "", "Release to install, e.g. 0.4.0 (required)")
	_ = update.MarkFlagRequired("version")
	controlCmd.AddCommand(update)
}

func runControl(req control.Request) error {
	if err := req.Validate(); err != nil {
		return err
	}
	var res control.Result
	if IsRemoteMode() {
		data, err := NewAPIClient().POST("/api/control", req)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &res); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	} else {
		cfg := GetConfig()
		redisAddr, err := models.BuildRedisConns(cfg)
		if err != nil {
			return fmt.Errorf("failed to connect to Redis: %w", err)
		}
		defer redisAddr.RDB.Close()
		db, err := models.BuildDB(cfg)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := control.Send(ctx, redisAddr.RDB, control.QueueExists(queueworker.NewStore(db, redisAddr.RDB)), req)
		if err != nil {
			return err
		}
		res = *out
	}

	fmt.Printf("Sent %s to %s (id %s, %d receiver(s)).\n", res.Action, res.Target, res.ID, res.Receivers)
	if res.Receivers == 0 {
		fmt.Println("No worker is listening on that channel: workers older than runqy-worker v0.4.0 ignore control commands.")
	} else {
		fmt.Println("Check the outcome with: runqy worker info <worker_id>")
	}
	return nil
}
