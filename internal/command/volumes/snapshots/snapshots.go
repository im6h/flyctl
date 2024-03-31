package snapshots

import (
	"github.com/spf13/cobra"

	"github.com/superfly/flyctl/internal/command"
)

func New() *cobra.Command {
	const (
		long = `"Commands for managing volume snapshots"
`
		short = "Manage volume snapshots"
		usage = "snapshots"
	)

	snapshots := command.New(usage, short, long, nil,
		command.RequireSession,
	)

	snapshots.Aliases = []string{"snapshot", "snaps"}

	snapshots.AddCommand(
		newList(),
		newCreate(),
	)

	return snapshots
}
