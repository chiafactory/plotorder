package cmd

import (
	"chiafactory/plotorder/client"
	"chiafactory/plotorder/processor"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	configFile         string
	apiKey             string
	apiURL             string
	orderID            string
	plotDir            string
	plotCheckFrequency time.Duration
	verbose            bool
	rootCmd            = &cobra.Command{
		Use:   "plotorder",
		Short: "plotorder automates the download of Chia plots from chiafactory.com",
		Run: func(cmd *cobra.Command, args []string) {

			if verbose {
				log.SetLevel(log.DebugLevel)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c := make(chan os.Signal)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)

			go func() {
				<-c
				log.Infof("shutting down")
				cancel()
			}()

			if apiKey == "" {
				log.Error("--api-key is required")
				return
			}

			if orderID == "" {
				log.Error("--order-id is required")
				return
			}

			if plotDir == "" {
				cwd, err := os.Getwd()
				if err != nil {
					log.Error("--plot-dir was not provided and we could not choose a default directory to store the plot files. Please provide --plot-dir")
					return
				}

				plotDir = path.Join(cwd, "plots")

				if _, err := os.Stat(plotDir); err != nil {
					if err = os.Mkdir(plotDir, os.ModePerm); err != nil {
						log.Errorf("--plot-dir was not provided and we could not create a default location (%s). Please provide --plot-dir", plotDir)
						return
					}
				}
			}

			log.Infof("apiKey=%s, apiURL=%s, plotDir=%s", fmt.Sprintf("****%s", apiKey[len(apiKey)-4:]), apiURL, plotDir)

			client := client.NewClient(apiKey, apiURL)
			proc, err := processor.NewProcessor(client, plotDir, plotCheckFrequency)
			if err != nil {
				log.Error("plot processing could not start")
				return
			}

			done, err := proc.Start(ctx, orderID)
			if err != nil {
				log.Errorf("error while starting to process plots for order (%s)", orderID)
				return
			}

			<-done
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
	log.SetFormatter(&log.TextFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)

	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "your personal https://chiafactory.com API key")
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "https://chiafactory.com/api/v1", "the URL of Chiafactory's API")
	rootCmd.PersistentFlags().StringVar(&orderID, "order-id", "", "the id of the order you want to process plots for")
	rootCmd.PersistentFlags().StringVar(&plotDir, "plot-dir", "", "the path where to store downloaded plots")
	rootCmd.PersistentFlags().DurationVar(&plotCheckFrequency, "plot-check-frequency", 2*time.Second, "the time between checks on an order's plots")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "config file to use")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enables verbose logging (DEBUG level)")

	viper.BindPFlag("api-key", rootCmd.PersistentFlags().Lookup("api-key"))
	viper.BindPFlag("api-url", rootCmd.PersistentFlags().Lookup("api-url"))
	viper.BindPFlag("plot-dir", rootCmd.PersistentFlags().Lookup("plot-dir"))
	viper.BindPFlag("plot-check-frequency", rootCmd.PersistentFlags().Lookup("check-frequency"))
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
