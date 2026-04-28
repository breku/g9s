package cmd

import (
	"os"

	"github.com/brekol/g9s/internal/config"
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
	logFile  *os.File
)

var rootCmd = &cobra.Command{
	Use:   "g9s",
	Short: "A terminal dashboard for GCP resources",
	Long: `g9s is a terminal UI for browsing and managing Google Cloud Platform
resources. Similar to k9s but for GCP.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Debug().Msg("starting g9s")
		cfg, err := config.Load()
		if err != nil {
			log.Error().Err(err).Msg("failed to load config")
			return err
		}
		// Redirect os.Stderr to the log file so that any third-party
		// library (gRPC, oauth2, etc.) writing directly to stderr does
		// not corrupt the tview screen.
		if logFile != nil {
			os.Stderr = logFile
		}
		app := ui.New(cfg)
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

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.config/g9s/config.yaml)")
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
		viper.AddConfigPath(home + "/.config/g9s")
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("G9S")
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

	// The TUI owns stdout/stderr — writing log lines there corrupts the
	// tview screen. Send all logs to a file under the user cache dir.
	logFile = openLogFile()
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: logFile, NoColor: true})
}

// openLogFile returns a writable log file under the user cache dir, falling
// back to /dev/null on any error so that logging never interferes with the TUI.
func openLogFile() *os.File {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return discardFile()
	}
	dir := cacheDir + "/g9s"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return discardFile()
	}
	f, err := os.OpenFile(dir+"/g9s.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return discardFile()
	}
	return f
}

func discardFile() *os.File {
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return nil
	}
	return f
}
