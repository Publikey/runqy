package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Publikey/runqy/alerts"
	"github.com/Publikey/runqy/models"
	"github.com/Publikey/runqy/vaults"
	"github.com/spf13/cobra"
)

var alertsHistoryLimit int

var alertsCmd = &cobra.Command{
	Use:   "alerts",
	Short: "Alerting commands",
	Long: `Inspect alerts and test email delivery.

Alert settings (provider, recipients, default rules) are configured with a top-level
'alerts' block in a queue YAML file, or from the dashboard's Alerts page. Per-queue
overrides live in each queue's 'alerts' block.

Examples:
  runqy alerts status
  runqy alerts status --history 100
  runqy alerts test

Remote mode:
  runqy --server https://runqy.example.com -k API_KEY alerts status`,
}

var alertsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active alerts and recent notifications",
	RunE:  runAlertsStatus,
}

var alertsTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Send a test email with the saved alert settings",
	RunE:  runAlertsTest,
}

func init() {
	rootCmd.AddCommand(alertsCmd)
	alertsCmd.AddCommand(alertsStatusCmd)
	alertsCmd.AddCommand(alertsTestCmd)
	alertsStatusCmd.Flags().IntVar(&alertsHistoryLimit, "history", 20, "Number of recent notifications to show")
}

func runAlertsStatus(cmd *cobra.Command, args []string) error {
	var overview alerts.OverviewResponse
	if IsRemoteMode() {
		data, err := NewAPIClient().GET(fmt.Sprintf("/api/alerts?history=%d", alertsHistoryLimit))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &overview); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
	} else {
		cfg := GetConfig()
		db, err := models.BuildDB(cfg)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer db.Close()
		redisAddr, err := models.BuildRedisConns(cfg)
		if err != nil {
			return fmt.Errorf("failed to connect to Redis: %w", err)
		}
		defer redisAddr.RDB.Close()
		ctx := context.Background()

		settings, err := alerts.NewSettingsStore(db).Get(ctx)
		if err != nil {
			return err
		}
		overview.Enabled = settings != nil && settings.Enabled
		if overview.Active, err = alerts.Active(ctx, redisAddr.RDB); err != nil {
			return err
		}
		if overview.History, err = alerts.History(ctx, redisAddr.RDB, int64(alertsHistoryLimit)); err != nil {
			return err
		}
		overview.Status, _ = alerts.GetStatus(ctx, redisAddr.RDB)
	}
	printAlertsOverview(overview)
	return nil
}

func printAlertsOverview(o alerts.OverviewResponse) {
	state := "disabled"
	if o.Enabled {
		state = "enabled"
	}
	fmt.Printf("Alerting: %s\n", state)
	if !o.Status.LastEval.IsZero() {
		fmt.Printf("Last evaluation: %s\n", o.Status.LastEval.Local().Format(time.RFC3339))
	}
	if o.Status.LastError != "" {
		fmt.Printf("Last error (%s): %s\n", o.Status.LastErrorAt.Local().Format(time.RFC3339), o.Status.LastError)
	}

	fmt.Println()
	if len(o.Active) == 0 {
		fmt.Println("No active alerts.")
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "STATE\tQUEUE\tRULE\tSINCE\tDETAILS")
		for _, a := range o.Active {
			st := "pending"
			if a.Firing {
				st = "FIRING"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", st, a.Queue, a.Rule, time.Since(a.Since).Round(time.Second), a.Message)
		}
		w.Flush()
	}

	if len(o.History) > 0 {
		fmt.Println("\nRecent notifications:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TIME\tSTATUS\tQUEUE\tRULE\tDETAILS")
		for _, h := range o.History {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", h.Time.Local().Format("2006-01-02 15:04"), strings.ToUpper(h.Status), h.Queue, h.Rule, h.Message)
		}
		w.Flush()
	}
}

func runAlertsTest(cmd *cobra.Command, args []string) error {
	if IsRemoteMode() {
		if _, err := NewAPIClient().POST("/api/alerts/test", nil); err != nil {
			return err
		}
		fmt.Println("Test email sent.")
		return nil
	}

	cfg := GetConfig()
	db, err := models.BuildDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	settings, err := alerts.NewSettingsStore(db).Get(ctx)
	if err != nil {
		return err
	}
	if settings == nil {
		return fmt.Errorf("alerting is not configured: add an 'alerts' block to a queue YAML file or use the dashboard")
	}
	if err := alerts.SendTest(ctx, settings, vaults.NewStore(db), os.Getenv("RUNQY_PUBLIC_URL")); err != nil {
		return fmt.Errorf("test email failed: %w", err)
	}
	fmt.Printf("Test email sent to %s.\n", strings.Join(settings.Recipients, ", "))
	return nil
}
