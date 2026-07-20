package main

import (
	"fmt"
	"strings"

	"github.com/pinchtab/pinchtab/internal/bridgeregistry"
	"github.com/pinchtab/pinchtab/internal/server"
	"github.com/spf13/cobra"
)

var (
	bridgeEngine    string
	bridgeCDPAttach string
	bridgeBind      string
	bridgePort      string
	bridgeStopPort  string
)

var bridgeCmd = &cobra.Command{
	Use:   "bridge",
	Short: "Start single-instance bridge-only server",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadConfig()
		engineMode, err := resolveBridgeEngine(bridgeEngine, cfg.Engine)
		if err != nil {
			return err
		}
		cfg.Engine = engineMode
		if v := strings.TrimSpace(bridgeCDPAttach); v != "" {
			cfg.CDPAttachURL = v
		}
		if v := strings.TrimSpace(bridgeBind); v != "" {
			cfg.Bind = v
		}
		if v := strings.TrimSpace(bridgePort); v != "" {
			cfg.Port = v
		}
		if cfg.CDPAttachURL != "" {
			entry, err := bridgeregistry.Register(cfg.CDPAttachURL, cfg.Port)
			if err != nil {
				return err
			}
			defer func() { _ = bridgeregistry.Remove(entry) }()
		}
		server.RunBridgeServer(cfg, version)
		return nil
	},
}

var bridgeStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a standalone bridge and remove its registry entry",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadConfig()
		if v := strings.TrimSpace(bridgeStopPort); v != "" {
			cfg.Port = v
		}
		if err := server.ShutdownServer(cfg.Port, cfg.Token); err != nil {
			return fmt.Errorf("stop bridge on port %s: %w", cfg.Port, err)
		}
		fmt.Printf("Stopped bridge on port %s\n", cfg.Port)
		return nil
	},
}

func resolveBridgeEngine(flagValue, configValue string) (string, error) {
	engineMode := strings.ToLower(strings.TrimSpace(configValue))
	if strings.TrimSpace(flagValue) != "" {
		engineMode = strings.ToLower(strings.TrimSpace(flagValue))
	}
	if engineMode == "" {
		engineMode = "chrome"
	}
	if engineMode != "chrome" && engineMode != "lite" && engineMode != "auto" {
		return "", fmt.Errorf("invalid --engine %q (expected chrome, lite, or auto)", engineMode)
	}
	return engineMode, nil
}

func init() {
	bridgeCmd.GroupID = "primary"
	bridgeCmd.Flags().StringVar(&bridgeEngine, "engine", "", "Bridge engine: chrome, lite, or auto (overrides config)")
	bridgeCmd.Flags().StringVar(&bridgeCDPAttach, "cdp-attach", "", "Attach to an existing Chrome via its browser-level CDP URL (e.g. ws://127.0.0.1:9222/devtools/browser/abc). Skips launching Chrome; the external Chrome is left alive on shutdown.")
	bridgeCmd.Flags().StringVar(&bridgeBind, "bind", "", "Bind address for the bridge HTTP server (overrides config server.bind)")
	bridgeCmd.Flags().StringVar(&bridgePort, "port", "", "Port for the bridge HTTP server (overrides config server.port)")
	bridgeStopCmd.Flags().StringVar(&bridgeStopPort, "port", "", "Port of the standalone bridge to stop")
	bridgeCmd.AddCommand(bridgeStopCmd)
	rootCmd.AddCommand(bridgeCmd)
}
