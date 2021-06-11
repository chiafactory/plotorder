package cmd

import (
	"bufio"
	"chiafactory/plotorder/client"
	"chiafactory/plotorder/processor"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	configFile         string
	apiKey             string
	apiURL             string
	orderID            string
	plotDirs           []string
	logsDir            string
	plotCheckFrequency time.Duration
	verbose            bool
	rootCmd            = &cobra.Command{
		Use:   "plotorder",
		Short: "plotorder automates the download of Chia plots from chiafactory.com",
		Run: func(cmd *cobra.Command, args []string) {

			if verbose {
				log.SetLevel(log.DebugLevel)
			}

			reporter := processor.NewReporter()
			reporter.Start()
			defer reporter.Stop()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c := make(chan os.Signal)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)

			go func() {
				<-c
				log.Infof("shutting down")
				cancel()
			}()

			go func() {
				scanner := bufio.NewScanner(os.Stdin)
				for {
					for scanner.Scan() {
						if scanner.Text() == "q" {
							c <- os.Kill
							break
						}
					}
				}
			}()

			if apiKey == "" {
				log.Error("--api-key is required")
				return
			}

			if orderID == "" {
				log.Error("--order-id is required")
				return
			}

			if len(plotDirs) == 0 {
				cwd, err := os.Getwd()
				if err != nil {
					log.Error("--plot-dir was not provided and we could not choose a default directory to store the plot files. Please provide --plot-dir")
					return
				}

				plotDirs = []string{path.Join(cwd, "plots")}
			}

			for _, plotDir := range plotDirs {
				if _, err := os.Stat(plotDir); err != nil {
					log.Warnf("the plot download directory (%s) does not exist, so we're creating it", plotDir)
					if err = os.Mkdir(plotDir, os.ModePerm); err != nil {
						log.Errorf("the plot download directory did not exist (%s) and we could not create it", plotDir)
						return
					}
				}
			}

			if logsDir == "" {
				cwd, err := os.Getwd()
				if err != nil {
					log.Error("--logs-dir was not provided and we could not choose a default directory to store the log files. Please provide --logs-dir")
					return
				}

				logsDir = path.Join(cwd, "logs")
			}

			if _, err := os.Stat(logsDir); err != nil {
				log.Warnf("the logs directory (%s) does not exist, so we're creating it", logsDir)
				if err = os.Mkdir(logsDir, os.ModePerm); err != nil {
					log.Errorf("the logs directory did not exist (%s) and we could not create it", logsDir)
					return
				}
			}

			// we're using the reporter and a log file writer. The reporter will
			// write to stdout until the first render
			log.SetOutput(
				io.MultiWriter(
					&lumberjack.Logger{
						Filename: path.Join(logsDir, "plotorder.log"),
						MaxSize:  256,
						MaxAge:   30,
						Compress: true,
					},
					reporter,
				),
			)

			log.Infof("apiKey=%s, apiURL=%s, plotDirs=%s, logsDir=%s", fmt.Sprintf("****%s", apiKey[len(apiKey)-4:]), apiURL, plotDirs, logsDir)

			client := client.NewClient(apiKey, apiURL)
			proc, err := processor.NewProcessor(client, reporter, plotDirs, plotCheckFrequency)
			if err != nil {
				log.Error("plot processing could not start")
				return
			}

			log.Infof("Loading plots, please wait")
			err = proc.Start(ctx, orderID)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Errorf("error in the plot processor: %s", err.Error())
				return
			}

			os.Exit(0)
		},
	}
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)

	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "your personal https://chiafactory.com API key")
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "https://chiafactory.com/api/v1", "the URL of Chiafactory's API")
	rootCmd.PersistentFlags().StringVar(&orderID, "order-id", "", "the id of the order you want to process plots for")
	rootCmd.PersistentFlags().StringArrayVar(&plotDirs, "plot-dir", []string{}, "the paths where to store downloaded plots")
	rootCmd.PersistentFlags().DurationVar(&plotCheckFrequency, "plot-check-frequency", 5*time.Second, "the time between checks on an order's plots")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "config file to use")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enables verbose logging (DEBUG level)")
	rootCmd.PersistentFlags().StringVar(&logsDir, "logs-dir", "", "the paths where to store downloaded plots")

	viper.BindPFlag("api-key", rootCmd.PersistentFlags().Lookup("api-key"))
	viper.BindPFlag("api-url", rootCmd.PersistentFlags().Lookup("api-url"))
	viper.BindPFlag("plot-dir", rootCmd.PersistentFlags().Lookup("plot-dir"))
	viper.BindPFlag("plot-check-frequency", rootCmd.PersistentFlags().Lookup("check-frequency"))
	viper.BindPFlag("logs-dir", rootCmd.PersistentFlags().Lookup("logs-dir"))
}

func initConfig() {
	viper.SetConfigType("ini")

	var usingConfigFile bool
	if configFile != "" {
		viper.SetConfigFile(configFile)
		usingConfigFile = true
	} else {
		home, err := homedir.Dir()
		if err != nil {
			log.Errorf("there was an error initialising plotorder")
			return
		}

		if _, err := os.Stat(path.Join(home, ".plotorder")); err == nil {
			viper.AddConfigPath(home)
			viper.SetConfigName(".plotorder")
			usingConfigFile = true
		}
	}

	if usingConfigFile {
		viper.AutomaticEnv()

		if err := viper.ReadInConfig(); err == nil {
			log.Infof("using config file: %s", viper.ConfigFileUsed())
		} else {
			log.Errorf("There was a problem loading your config file (%s)", err.Error())
		}
	}
}
