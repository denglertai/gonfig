package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resetFlags resets the flags of all commands to their defaults, as flag values persist between executions of rootCmd
func resetFlags(t *testing.T) {
	t.Helper()

	var reset func(*cobra.Command)
	reset = func(c *cobra.Command) {
		for _, flags := range []*pflag.FlagSet{c.Flags(), c.PersistentFlags()} {
			flags.VisitAll(func(f *pflag.Flag) {
				if err := f.Value.Set(f.DefValue); err != nil {
					t.Fatalf("failed to reset flag %s: %v", f.Name, err)
				}
				f.Changed = false
			})
		}
		for _, sub := range c.Commands() {
			reset(sub)
		}
	}

	reset(rootCmd)
}
