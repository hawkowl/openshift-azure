package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/openshift/installer/pkg/asset/store"
	"github.com/openshift/installer/pkg/terraform/exec/plugins"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/openshift/openshift-azure/pkg/api"
	fakerpconfig "github.com/openshift/openshift-azure/pkg/fakerp/config"
)

var (
	gitCommit = "unknown"
)

func main() {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	logrus.SetReportCaller(true)
	log := logrus.NewEntry(logrus.StandardLogger())
	// TODO: This should move to installer /pkg
	if len(os.Args) > 0 {
		base := filepath.Base(os.Args[0])
		cname := strings.TrimSuffix(base, filepath.Ext(base))
		if pluginRunner, ok := plugins.KnownPlugins[cname]; ok {
			pluginRunner()
			return
		}
	}

	if err := run(log); err != nil {
		panic(err)
	}
}

func run(log *logrus.Entry) error {
	rootCmd := &cobra.Command{
		Use:  "./azure [component]",
		Long: "Azure Red Hat OpenShift dispatcher",
	}
	rootCmd.PersistentFlags().StringP("action", "a", "Create", "Valid values are [Create, Delete]")
	rootCmd.PersistentFlags().StringP("name", "n", "demo-cluster", "Cluster name value")
	rootCmd.Printf("gitCommit %s\n", gitCommit)
	rootCmd.Execute()

	name, err := rootCmd.Flags().GetString("name")
	if err != nil {
		return err
	}

	action, err := rootCmd.Flags().GetString("action")
	if err != nil {
		return err
	}
	// env variable configuration
	ec, err := fakerpconfig.NewEnvConfig(name)
	if err != nil {
		return err
	}

	// assetStore is responsible for all assets execution
	// TODO: directory and assetStore should get new SQL DB based implementation
	assetStore, err := store.NewStore(ec.Directory)
	if err != nil {
		return errors.Wrap(err, "failed to create asset store")
	}

	p := api.NewPlugin(ec.Directory, assetStore)

	ctx := context.Background()
	context.WithValue(ctx, api.ContextClientID, ec.ClientID)
	context.WithValue(ctx, api.ContextClientSecret, ec.ClientSecret)
	context.WithValue(ctx, api.ContextTenantID, ec.TenantID)
	context.WithValue(ctx, api.ContextSubscriptionID, ec.SubscriptionID)

	if action == "Create" {
		cfg, err := p.GenerateConfig(ctx, name)
		if err != nil {
			return err
		}

		err = fakerpconfig.EnrichInstallConfig(name, ec, cfg)
		if err != nil {
			return errors.Wrap(err, "failed to enrich InstallConfig")
		}

		return p.Create(ctx, name, cfg)
	}
	return p.Delete(ctx, name)
}
