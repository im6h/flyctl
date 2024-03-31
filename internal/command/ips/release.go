package ips

import (
	"context"
	"fmt"
	"net"

	"github.com/spf13/cobra"
	fly "github.com/superfly/fly-go"
	"github.com/superfly/flyctl/internal/appconfig"
	"github.com/superfly/flyctl/internal/command"
	"github.com/superfly/flyctl/internal/flag"
)

func newRelease() *cobra.Command {
	const (
		long  = `Releases one or more IP addresses from the application`
		short = `Release IP addresses`
	)

	cmd := command.New("release [flags] ADDRESS ADDRESS ...", short, long, runReleaseIPAddress,
		command.RequireSession,
		command.RequireAppName,
	)

	flag.Add(cmd,
		flag.App(),
		flag.AppConfig(),
	)

	cmd.Args = cobra.MinimumNArgs(1)
	return cmd
}

func runReleaseIPAddress(ctx context.Context) error {
	client := fly.ClientFromContext(ctx)

	appName := appconfig.NameFromContext(ctx)

	for _, address := range flag.Args(ctx) {

		if ip := net.ParseIP(address); ip == nil {
			return fmt.Errorf("Invalid IP address: '%s'", address)
		}

		if err := client.ReleaseIPAddress(ctx, appName, address); err != nil {
			return err
		}

		fmt.Printf("Released %s from %s\n", address, appName)
	}

	return nil
}
