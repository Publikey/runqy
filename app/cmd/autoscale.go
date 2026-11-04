package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Publikey/runqy/autoscale"
	"github.com/Publikey/runqy/autoscale/provider"
	"github.com/Publikey/runqy/models"
	"github.com/spf13/cobra"
)

var (
	providerType    string
	providerConfig  string
	providerDisable bool
	providerEnable  bool
	autoscaleForce  bool
	autoscaleAll    bool
)

var autoscaleCmd = &cobra.Command{
	Use:   "autoscale",
	Short: "GPU autoscaling management commands",
	Long: `Manage GPU worker autoscaling: inspect live instances, manage cloud provider
configurations, and protect instances from scale-down.

Per-queue autoscaling rules live in the queue config (YAML/API/UI). These commands manage
the provider registry, live instance status, and instance protection.

Examples:
  runqy autoscale status          # active instances + provisioning pauses
  runqy autoscale status --all    # also failed/terminated instances, with the reason
  runqy autoscale logs as-1234    # machine logs (live, or saved when it failed)
  runqy autoscale provider-types
  runqy autoscale providers list
  runqy autoscale providers create my-mock --type mock
  runqy autoscale providers show my-mock
  runqy autoscale providers delete my-mock --force
  runqy autoscale protect as-1234
  runqy autoscale unprotect as-1234

Remote mode:
  runqy --server https://runqy.example.com -k API_KEY autoscale status`,
}

var autoscaleStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active autoscaler instances, provisioning pauses and cost",
	RunE:  runAutoscaleStatus,
}

var autoscaleLogsCmd = &cobra.Command{
	Use:   "logs <instance-id>",
	Short: "Show an instance's machine logs (live from the provider, or saved at teardown)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAutoscaleLogs,
}

var autoscaleProtectCmd = &cobra.Command{
	Use:   "protect <instance-id>",
	Short: "Protect an instance from scale-down",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runAutoscaleSetProtected(args[0], true) },
}

var autoscaleUnprotectCmd = &cobra.Command{
	Use:   "unprotect <instance-id>",
	Short: "Remove scale-down protection from an instance",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runAutoscaleSetProtected(args[0], false) },
}

var autoscaleProviderTypesCmd = &cobra.Command{
	Use:   "provider-types",
	Short: "List registered provider types",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(strings.Join(provider.Types(), "\n"))
		return nil
	},
}

var autoscaleProvidersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Manage autoscale provider configurations",
}

var autoscaleProvidersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List provider configurations",
	RunE:  runAutoscaleProvidersList,
}

var autoscaleProvidersShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show a provider configuration (secrets masked)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAutoscaleProvidersShow,
}

var autoscaleProvidersCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a provider configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runAutoscaleProvidersCreate,
}

var autoscaleProvidersUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update a provider configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runAutoscaleProvidersUpdate,
}

var autoscaleProvidersDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a provider configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runAutoscaleProvidersDelete,
}

func init() {
	rootCmd.AddCommand(autoscaleCmd)
	autoscaleCmd.AddCommand(autoscaleStatusCmd)
	autoscaleCmd.AddCommand(autoscaleLogsCmd)
	autoscaleStatusCmd.Flags().BoolVar(&autoscaleAll, "all", false, "Also list failed and terminated instances")
	autoscaleCmd.AddCommand(autoscaleProtectCmd)
	autoscaleCmd.AddCommand(autoscaleUnprotectCmd)
	autoscaleCmd.AddCommand(autoscaleProviderTypesCmd)
	autoscaleCmd.AddCommand(autoscaleProvidersCmd)
	autoscaleProvidersCmd.AddCommand(autoscaleProvidersListCmd)
	autoscaleProvidersCmd.AddCommand(autoscaleProvidersShowCmd)
	autoscaleProvidersCmd.AddCommand(autoscaleProvidersCreateCmd)
	autoscaleProvidersCmd.AddCommand(autoscaleProvidersUpdateCmd)
	autoscaleProvidersCmd.AddCommand(autoscaleProvidersDeleteCmd)

	autoscaleProvidersCreateCmd.Flags().StringVar(&providerType, "type", "", "Provider type (e.g. mock); see 'autoscale provider-types'")
	autoscaleProvidersCreateCmd.Flags().StringVar(&providerConfig, "config", "", "Provider config JSON (literal or @file)")
	autoscaleProvidersCreateCmd.Flags().BoolVar(&providerDisable, "disabled", false, "Create the provider disabled")

	autoscaleProvidersUpdateCmd.Flags().StringVar(&providerType, "type", "", "Provider type")
	autoscaleProvidersUpdateCmd.Flags().StringVar(&providerConfig, "config", "", "Provider config JSON (literal or @file)")
	autoscaleProvidersUpdateCmd.Flags().BoolVar(&providerEnable, "enable", false, "Enable the provider")
	autoscaleProvidersUpdateCmd.Flags().BoolVar(&providerDisable, "disable", false, "Disable the provider")

	autoscaleProvidersDeleteCmd.Flags().BoolVar(&autoscaleForce, "force", false, "Skip confirmation")
}

// --- local store helpers ---

func getAutoscaleInstanceStore() (*autoscale.Store, error) {
	cfg := GetConfig()
	db, err := models.BuildDB(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return autoscale.NewStore(db), nil
}

func getAutoscaleProviderStore() (*provider.Store, error) {
	cfg := GetConfig()
	db, err := models.BuildDB(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return provider.NewStore(db), nil
}

const providersDisabledHint = "autoscale providers require encryption. Set RUNQY_VAULT_MASTER_KEY.\n  Generate a key with: openssl rand -base64 32"

// --- status ---

// printInstanceRow writes one `autoscale status` row: the GPU and hourly price are what
// the provider actually rented.
func printInstanceRow(w io.Writer, i *autoscale.Instance) {
	gpu := i.GPU
	if gpu == "" {
		gpu = "-"
	}
	reason := i.Error
	if reason == "" {
		reason = i.Reason
	}
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%v\t$%.3f\t$%.4f\t%s\n", i.InstanceID, i.Queue, i.Provider, gpu,
		autoscale.DisplayStatus(i.Status, i.WorkerID), i.WorkerID, i.Protected, i.PricePerHour, i.CostAccumulated, reason)
}

func runAutoscaleStatus(cmd *cobra.Command, args []string) error {
	var (
		instances []*autoscale.Instance
		pauses    []autoscale.Pause
		total     float64
	)
	if IsRemoteMode() {
		resp, err := NewAPIClient().AutoscaleStatusAPI()
		if err != nil {
			return err
		}
		for _, i := range resp.Instances {
			instances = append(instances, &autoscale.Instance{InstanceID: i.InstanceID, Queue: i.Queue, Provider: i.Provider, GPU: i.GPU,
				Status: i.Status, WorkerID: i.WorkerID, Protected: i.Protected, PricePerHour: i.PricePerHour, CostAccumulated: i.CostAccumulated,
				Reason: i.Reason, Error: i.Error})
		}
		pauses, total = resp.Pauses, resp.TotalCost
	} else {
		store, err := getAutoscaleInstanceStore()
		if err != nil {
			return err
		}
		instances, err = store.ListAll(context.Background())
		if err != nil {
			return err
		}
		for _, i := range instances {
			total += i.CostAccumulated
		}
		if conns, err := models.BuildRedisConns(GetConfig()); err == nil {
			pauses, _ = autoscale.ListPauses(context.Background(), conns.RDB, time.Now())
			conns.RDB.Close()
		}
	}

	shown, hidden := 0, 0
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, i := range instances {
		ended := i.Status == autoscale.StatusFailed || i.Status == autoscale.StatusTerminated
		if ended && !autoscaleAll {
			hidden++
			continue
		}
		if shown == 0 {
			fmt.Fprintln(w, "INSTANCE ID\tQUEUE\tPROVIDER\tGPU\tSTATUS\tWORKER\tPROTECTED\tPRICE/HR\tCOST\tREASON")
		}
		printInstanceRow(w, i)
		shown++
	}
	w.Flush()
	if shown == 0 {
		fmt.Println("No active autoscaler instances.")
	}
	if hidden > 0 {
		fmt.Printf("(%d failed/terminated instance(s) hidden, use --all)\n", hidden)
	}

	if len(pauses) > 0 {
		fmt.Println("\nProvisioning paused:")
		pw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(pw, "QUEUE\tUNTIL\tFAILURES\tCAUSE")
		for _, p := range pauses {
			fmt.Fprintf(pw, "%s\t%s (in %s)\t%d\t%s\n", p.Queue, p.Until.Local().Format("15:04:05"),
				time.Until(p.Until).Round(time.Second), p.Failures, p.Cause)
		}
		pw.Flush()
	}
	fmt.Printf("\nTotal accumulated cost: $%.4f\n", total)
	return nil
}

func runAutoscaleLogs(cmd *cobra.Command, args []string) error {
	var (
		logs *autoscale.InstanceLogs
		err  error
	)
	if IsRemoteMode() {
		logs, err = NewAPIClient().InstanceLogsAPI(args[0])
	} else {
		instStore, serr := getAutoscaleInstanceStore()
		if serr != nil {
			return serr
		}
		provStore, serr := getAutoscaleProviderStore()
		if serr != nil {
			return serr
		}
		logs, err = autoscale.FetchInstanceLogs(context.Background(), instStore, provStore, args[0])
	}
	if err != nil {
		return err
	}
	switch {
	case logs.Source == "live":
		fmt.Fprintf(os.Stderr, "# live logs of %s, fetched %s\n", logs.InstanceID, logs.FetchedAt)
	case logs.Logs == "" && logs.Error != "":
		return fmt.Errorf("logs could not be captured before teardown: %s", logs.Error)
	case logs.Logs == "":
		fmt.Println("No saved logs for this instance (captured only for instances that failed after renting a machine).")
		return nil
	default:
		fmt.Fprintf(os.Stderr, "# logs of %s saved at teardown (%s)\n", logs.InstanceID, logs.FetchedAt)
	}
	fmt.Print(logs.Logs)
	if !strings.HasSuffix(logs.Logs, "\n") {
		fmt.Println()
	}
	return nil
}

// --- protect / unprotect ---

func runAutoscaleSetProtected(instanceID string, protected bool) error {
	if IsRemoteMode() {
		if err := NewAPIClient().SetInstanceProtectedAPI(instanceID, protected); err != nil {
			return err
		}
	} else {
		store, err := getAutoscaleInstanceStore()
		if err != nil {
			return err
		}
		if err := store.SetProtected(context.Background(), instanceID, protected); err != nil {
			return err
		}
	}
	state := "unprotected"
	if protected {
		state = "protected"
	}
	fmt.Printf("Instance '%s' %s.\n", instanceID, state)
	return nil
}

// --- providers ---

func runAutoscaleProvidersList(cmd *cobra.Command, args []string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tENABLED")

	if IsRemoteMode() {
		providers, err := NewAPIClient().ListAutoscaleProvidersAPI()
		if err != nil {
			return err
		}
		if len(providers) == 0 {
			fmt.Println("No providers configured.")
			return nil
		}
		for _, p := range providers {
			fmt.Fprintf(w, "%s\t%s\t%v\n", p.Name, p.ProviderType, p.Enabled)
		}
		w.Flush()
		return nil
	}

	store, err := getAutoscaleProviderStore()
	if err != nil {
		return err
	}
	if !store.IsEnabled() {
		return fmt.Errorf("%s", providersDisabledHint)
	}
	recs, err := store.List(context.Background())
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		fmt.Println("No providers configured.")
		return nil
	}
	for _, p := range recs {
		fmt.Fprintf(w, "%s\t%s\t%v\n", p.Name, p.ProviderType, p.Enabled)
	}
	w.Flush()
	return nil
}

func runAutoscaleProvidersShow(cmd *cobra.Command, args []string) error {
	name := args[0]
	var (
		ptype   string
		enabled bool
		cfgJSON json.RawMessage
	)
	if IsRemoteMode() {
		p, err := NewAPIClient().GetAutoscaleProviderAPI(name)
		if err != nil {
			return err
		}
		ptype, enabled, cfgJSON = p.ProviderType, p.Enabled, p.Config
	} else {
		store, err := getAutoscaleProviderStore()
		if err != nil {
			return err
		}
		if !store.IsEnabled() {
			return fmt.Errorf("%s", providersDisabledHint)
		}
		rec, err := store.Get(context.Background(), name)
		if err != nil {
			return err
		}
		if rec == nil {
			return fmt.Errorf("provider '%s' not found", name)
		}
		ptype, enabled, cfgJSON = rec.ProviderType, rec.Enabled, provider.MaskConfig(rec.Config)
	}
	fmt.Printf("Name:    %s\n", name)
	fmt.Printf("Type:    %s\n", ptype)
	fmt.Printf("Enabled: %v\n", enabled)
	fmt.Printf("Config:  %s\n", string(cfgJSON))
	return nil
}

func runAutoscaleProvidersCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if providerType == "" {
		return fmt.Errorf("--type is required (see 'runqy autoscale provider-types')")
	}
	cfg, err := readProviderConfig(providerConfig)
	if err != nil {
		return err
	}
	enabled := !providerDisable

	if IsRemoteMode() {
		if err := NewAPIClient().CreateAutoscaleProviderAPI(AutoscaleProviderRequestAPI{
			Name: name, ProviderType: providerType, Config: cfg, Enabled: &enabled,
		}); err != nil {
			return err
		}
		fmt.Printf("Provider '%s' created.\n", name)
		return nil
	}

	store, err := getAutoscaleProviderStore()
	if err != nil {
		return err
	}
	if !store.IsEnabled() {
		return fmt.Errorf("%s", providersDisabledHint)
	}
	if _, err := store.Create(context.Background(), name, providerType, cfg, enabled); err != nil {
		return err
	}
	fmt.Printf("Provider '%s' created.\n", name)
	return nil
}

func runAutoscaleProvidersUpdate(cmd *cobra.Command, args []string) error {
	name := args[0]
	// Only send what was asked to change: no --config keeps the stored config (and its
	// credentials), no --enable/--disable keeps the current enabled flag.
	var cfg json.RawMessage
	if strings.TrimSpace(providerConfig) != "" {
		var err error
		if cfg, err = readProviderConfig(providerConfig); err != nil {
			return err
		}
	}
	if providerEnable && providerDisable {
		return fmt.Errorf("--enable and --disable are mutually exclusive")
	}
	var enabled *bool
	if providerEnable || providerDisable {
		v := providerEnable
		enabled = &v
	}

	if IsRemoteMode() {
		req := AutoscaleProviderRequestAPI{ProviderType: providerType, Config: cfg, Enabled: enabled}
		if err := NewAPIClient().UpdateAutoscaleProviderAPI(name, req); err != nil {
			return err
		}
		fmt.Printf("Provider '%s' updated.\n", name)
		return nil
	}

	store, err := getAutoscaleProviderStore()
	if err != nil {
		return err
	}
	if !store.IsEnabled() {
		return fmt.Errorf("%s", providersDisabledHint)
	}
	if _, err := store.Update(context.Background(), name, providerType, cfg, enabled); err != nil {
		return err
	}
	fmt.Printf("Provider '%s' updated.\n", name)
	return nil
}

func runAutoscaleProvidersDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	if !autoscaleForce {
		fmt.Printf("Are you sure you want to delete provider '%s'? (y/N): ", name)
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	if IsRemoteMode() {
		if err := NewAPIClient().DeleteAutoscaleProviderAPI(name); err != nil {
			return err
		}
	} else {
		store, err := getAutoscaleProviderStore()
		if err != nil {
			return err
		}
		if !store.IsEnabled() {
			return fmt.Errorf("%s", providersDisabledHint)
		}
		if err := store.Delete(context.Background(), name); err != nil {
			return err
		}
	}
	fmt.Printf("Provider '%s' deleted.\n", name)
	return nil
}

// readProviderConfig reads provider config from a literal JSON string or @file path.
// Empty input yields an empty JSON object.
func readProviderConfig(in string) (json.RawMessage, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return json.RawMessage("{}"), nil
	}
	if strings.HasPrefix(in, "@") {
		b, err := os.ReadFile(in[1:])
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		in = string(b)
	}
	if !json.Valid([]byte(in)) {
		return nil, fmt.Errorf("config is not valid JSON")
	}
	return json.RawMessage(in), nil
}
