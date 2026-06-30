package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

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
)

var autoscaleCmd = &cobra.Command{
	Use:   "autoscale",
	Short: "GPU autoscaling management commands",
	Long: `Manage GPU worker autoscaling: inspect live instances, manage cloud provider
configurations, and protect instances from scale-down.

Per-queue autoscaling rules live in the queue config (YAML/API/UI). These commands manage
the provider registry, live instance status, and instance protection.

Examples:
  runqy autoscale status
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
	Short: "Show tracked autoscaler instances and cost",
	RunE:  runAutoscaleStatus,
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

func runAutoscaleStatus(cmd *cobra.Command, args []string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "INSTANCE ID\tQUEUE\tPROVIDER\tSTATUS\tWORKER\tPROTECTED\tCOST")

	if IsRemoteMode() {
		resp, err := NewAPIClient().AutoscaleStatusAPI()
		if err != nil {
			return err
		}
		if len(resp.Instances) == 0 {
			fmt.Println("No autoscaler instances.")
			return nil
		}
		for _, i := range resp.Instances {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%v\t$%.4f\n", i.InstanceID, i.Queue, i.Provider, i.Status, i.WorkerID, i.Protected, i.CostAccumulated)
		}
		w.Flush()
		fmt.Printf("\nTotal accumulated cost: $%.4f\n", resp.TotalCost)
		return nil
	}

	store, err := getAutoscaleInstanceStore()
	if err != nil {
		return err
	}
	instances, err := store.ListAll(context.Background())
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		fmt.Println("No autoscaler instances.")
		return nil
	}
	var total float64
	for _, i := range instances {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%v\t$%.4f\n", i.InstanceID, i.Queue, i.Provider, i.Status, i.WorkerID, i.Protected, i.CostAccumulated)
		total += i.CostAccumulated
	}
	w.Flush()
	fmt.Printf("\nTotal accumulated cost: $%.4f\n", total)
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
		ptype    string
		enabled  bool
		cfgJSON  json.RawMessage
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
	cfg, err := readProviderConfig(providerConfig)
	if err != nil {
		return err
	}
	// Default enabled=true unless --disable given; --enable forces true.
	enabled := true
	if providerDisable {
		enabled = false
	}

	if IsRemoteMode() {
		req := AutoscaleProviderRequestAPI{ProviderType: providerType, Config: cfg, Enabled: &enabled}
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
