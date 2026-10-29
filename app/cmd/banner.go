package cmd

import (
	"fmt"
	"strings"
)

// StartupConfig holds the configuration data to display in the startup banner
type StartupConfig struct {
	Version        string
	Port           string
	DatabaseType   string // "SQLite" or "PostgreSQL"
	DatabaseName   string
	RedisHost      string
	RedisConnected bool
	QueuesLoaded   int
	VaultsEnabled  bool
	UIEnabled      bool
	WatchEnabled   bool
	GitRepoURL     string
}

const asciiLogo = `  ______   __  __   __   __   ______   __  __
 /\  == \ /\ \/\ \ /\ "-.\ \ /\  __ \ /\ \_\ \
 \ \  __< \ \ \_\ \\ \ \-.  \\ \ \/\_\\ \____ \
  \ \_\ \_\\ \_____\\ \_\\"\_\\ \___\_\\/\_____\
   \/_/ /_/ \/_____/ \/_/ \/_/ \/___/_/ \/_____/`

// PrintStartupBanner prints the ASCII logo and configuration summary
func PrintStartupBanner(cfg StartupConfig) {
	// Format version string (add "v" prefix only if not already present and not a branch name)
	version := cfg.Version
	if !strings.HasPrefix(version, "v") && version != "dev" && version != "main" && version != "master" {
		version = "v" + version
	}

	// Print ASCII logo with version on the last line
	fmt.Println()
	lines := strings.Split(asciiLogo, "\n")
	for i, line := range lines {
		if i == len(lines)-1 { // Last line of logo
			fmt.Printf("%s  %s\n\n", line, version)
		} else {
			fmt.Println(line)
		}
	}

	// Server URL
	fmt.Printf("  Server: http://localhost:%s\n\n", cfg.Port)

	// Configuration summary
	// Database
	fmt.Printf("  Database     %s (%s)\n", cfg.DatabaseType, cfg.DatabaseName)

	// Redis
	redisStatus := "[connected]"
	if !cfg.RedisConnected {
		redisStatus = "[disconnected]"
	}
	fmt.Printf("  Redis        %s %s\n", cfg.RedisHost, redisStatus)

	// Queues
	fmt.Printf("  Queues       %d\n", cfg.QueuesLoaded)

	// Vaults
	if cfg.VaultsEnabled {
		fmt.Printf("  Vaults       enabled\n")
	} else {
		fmt.Printf("  Vaults       disabled\n")
	}

	// Endpoints (only if UI is enabled)
	if cfg.UIEnabled {
		fmt.Printf("  Monitoring   /monitoring\n")
	}
	fmt.Printf("  Metrics      /metrics\n")
	fmt.Printf("  Swagger      /swagger\n")

	// Watch mode
	if cfg.WatchEnabled {
		if cfg.GitRepoURL != "" {
			fmt.Printf("  Watch        enabled (git)\n")
		} else {
			fmt.Printf("  Watch        enabled (filesystem)\n")
		}
	}

	// Documentation link
	fmt.Printf("\n  Docs: https://runqy.com/docs/\n\n")
}
