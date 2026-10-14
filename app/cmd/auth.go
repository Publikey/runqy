package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var (
	// login flags
	loginServer   string
	loginAPIKey   string
	loginProfile  string
	loginNoSwitch bool

	// logout flags
	logoutProfile string
	logoutAll     bool
)

// loginCmd handles saving server credentials
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Save server credentials for remote mode",
	Long: `Login saves server credentials to ~/.runqy/credentials.json for later use.

After logging in, the profile becomes the active one: runqy commands use it without
--server and --api-key flags. Use --no-switch to save a profile without activating it.

Examples:
  runqy login --server https://production.example.com:3000 --api-key prod-api-key
  runqy login --server https://staging.example.com:3000 --name staging
  runqy login --server https://staging.example.com:3000 --name staging --no-switch`,
	RunE: runLogin,
}

// logoutCmd removes saved credentials
var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove saved credentials",
	Long: `Logout removes saved server credentials.

By default, removes the current profile. Use --name to specify a different profile,
or --all to remove all saved credentials.

Examples:
  runqy logout              # Remove current profile
  runqy logout --name staging  # Remove "staging" profile
  runqy logout --all        # Remove all saved credentials`,
	RunE: runLogout,
}

// authCmd is the parent for auth subcommands
var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication management commands",
	Long: `Authentication management commands for viewing and switching server profiles.

Examples:
  runqy auth status    Show current server connection
  runqy auth list      List all saved servers
  runqy auth switch    Switch to different saved server`,
}

// authStatusCmd shows current authentication status
var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current server connection",
	Long: `Show the current server connection status, including the active profile
and server URL.`,
	RunE: runAuthStatus,
}

// authListCmd lists all saved profiles
var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved servers",
	Long:  `List all saved server profiles with their URLs.`,
	RunE:  runAuthList,
}

// authSwitchCmd switches to a different profile
var authSwitchCmd = &cobra.Command{
	Use:   "switch <name>",
	Short: "Switch to different saved server",
	Long: `Switch the current active profile to a different saved server.

Example:
  runqy auth switch staging`,
	Args: cobra.ExactArgs(1),
	RunE: runAuthSwitch,
}

func init() {
	// login flags (using long form only to avoid conflict with global -s and -k flags)
	loginCmd.Flags().StringVarP(&loginServer, "server", "s", "", "Server URL (required)")
	loginCmd.Flags().StringVarP(&loginAPIKey, "api-key", "k", "", "API key (will prompt if not provided)")
	loginCmd.Flags().StringVarP(&loginProfile, "name", "n", "default", "Profile name")
	loginCmd.Flags().BoolVar(&loginNoSwitch, "no-switch", false, "Save the profile without making it the active one")
	loginCmd.MarkFlagRequired("server")

	// logout flags
	logoutCmd.Flags().StringVarP(&logoutProfile, "name", "n", "", "Profile to remove (default: current)")
	logoutCmd.Flags().BoolVar(&logoutAll, "all", false, "Remove all saved credentials")

	// Register commands
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authListCmd)
	authCmd.AddCommand(authSwitchCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	// If API key not provided via flag, prompt for it
	apiKeyToSave := loginAPIKey
	if apiKeyToSave == "" {
		// Check centralized config (includes env var)
		apiKeyToSave = GetConfig().APIKey
		if apiKeyToSave == "" {
			// Prompt user
			fmt.Print("API Key: ")
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read API key: %w", err)
			}
			apiKeyToSave = strings.TrimSpace(input)
			if apiKeyToSave == "" {
				return fmt.Errorf("API key is required")
			}
		}
	}

	// Normalize server URL (remove trailing slash)
	serverToSave := strings.TrimSuffix(loginServer, "/")

	// Save credentials
	if err := SaveCredentials(loginProfile, serverToSave, apiKeyToSave); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Printf("Logged in to %s (saved as \"%s\")\n", serverToSave, loginProfile)

	// Activate the profile just logged into, so the next commands go where the user
	// expects. Saving a second profile used to leave the previous one active silently,
	// sending commands to the wrong server (e.g. production instead of staging).
	if !loginNoSwitch {
		if err := SetCurrentProfile(loginProfile); err != nil {
			return fmt.Errorf("failed to activate profile: %w", err)
		}
	}
	current, err := GetCurrentProfileName()
	if err != nil {
		return fmt.Errorf("failed to read active profile: %w", err)
	}
	if current == loginProfile {
		fmt.Printf("Active profile: \"%s\"\n", current)
	} else {
		fmt.Printf("Active profile is still \"%s\" (switch with: runqy auth switch %s)\n", current, loginProfile)
	}
	return nil
}

func runLogout(cmd *cobra.Command, args []string) error {
	if logoutAll {
		if err := DeleteAllCredentials(); err != nil {
			return fmt.Errorf("failed to remove credentials: %w", err)
		}
		fmt.Println("Removed all saved credentials.")
		return nil
	}

	// Determine which profile to remove
	profileToRemove := logoutProfile
	if profileToRemove == "" {
		var err error
		profileToRemove, err = GetCurrentProfileName()
		if err != nil {
			return fmt.Errorf("failed to get current profile: %w", err)
		}
		if profileToRemove == "" {
			fmt.Println("Not logged in.")
			return nil
		}
	}

	if err := DeleteCredentials(profileToRemove); err != nil {
		return fmt.Errorf("failed to remove profile: %w", err)
	}

	fmt.Printf("Logged out from \"%s\"\n", profileToRemove)
	return nil
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	profileName, err := GetCurrentProfileName()
	if err != nil {
		return fmt.Errorf("failed to get auth status: %w", err)
	}

	if profileName == "" {
		fmt.Println("Not logged in.")
		fmt.Println()
		fmt.Println("Use 'runqy login --server <url> --api-key <key>' to save credentials.")
		return nil
	}

	creds, err := GetCurrentCredentials()
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}

	if creds == nil {
		fmt.Println("Not logged in.")
		return nil
	}

	fmt.Printf("Current server: %s\n", profileName)
	fmt.Printf("URL: %s\n", creds.URL)
	fmt.Printf("API Key: %s\n", MaskAPIKey(creds.APIKey))

	return nil
}

func runAuthList(cmd *cobra.Command, args []string) error {
	servers, current, err := ListCredentials()
	if err != nil {
		return fmt.Errorf("failed to list profiles: %w", err)
	}

	if len(servers) == 0 {
		fmt.Println("No saved servers.")
		fmt.Println()
		fmt.Println("Use 'runqy login --server <url> --api-key <key>' to save credentials.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tURL\tCURRENT")

	for name, creds := range servers {
		marker := ""
		if name == current {
			marker = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, creds.URL, marker)
	}

	w.Flush()
	return nil
}

func runAuthSwitch(cmd *cobra.Command, args []string) error {
	if err := validateArgs(args, "profile name"); err != nil {
		return err
	}
	profileName := args[0]

	if err := SetCurrentProfile(profileName); err != nil {
		return fmt.Errorf("failed to switch profile: %w", err)
	}

	// Show the new server details
	creds, err := GetCurrentCredentials()
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}

	fmt.Printf("Switched to \"%s\"\n", profileName)
	fmt.Printf("URL: %s\n", creds.URL)

	return nil
}
