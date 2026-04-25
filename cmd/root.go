package cmd

import (
	"os"

	"github.com/brekol/g9s/internal/ui"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile  string
	logLevel string
	project  string
)

var rootCmd = &cobra.Command{
	Use:   "gcptui",
	Short: "A terminal dashboard for GCP resources",
	Long: `gcptui is a terminal UI for browsing and managing Google Cloud Platform
resources. Similar to k9s but for GCP.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Debug().Msg("starting gcptui")
		app := ui.New()
		return app.Run()
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig, initLogger)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.config/gcptui/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (trace|debug|info|warn|error)")
	rootCmd.PersistentFlags().StringVarP(&project, "project", "p", "", "GCP project ID")

	_ = viper.BindPFlag("project", rootCmd.PersistentFlags().Lookup("project"))
	_ = viper.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log-level"))

	rootCmd.AddCommand(versionCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, _ := os.UserHomeDir()
		viper.AddConfigPath(home + "/.config/gcptui")
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("GCPTUI")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		log.Debug().Str("config", viper.ConfigFileUsed()).Msg("loaded config file")
	}
}

func initLogger() {
	level, err := zerolog.ParseLevel(viper.GetString("log_level"))
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
}
