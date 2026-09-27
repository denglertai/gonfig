/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/denglertai/gonfig/internal/value"
	"github.com/spf13/cobra"
)

// valueCmd represents the value command
var valueCmd = &cobra.Command{
	Use:   "value",
	Short: "Extract or process a specific configuration value",
	Long: `The value command allows you to extract and process individual configuration values.
It supports various filters for transforming the output, such as uppercasing, lowercasing,
trimming whitespace, or applying cryptographic functions like bcrypt or md5.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, arg := range args {
			result, err := value.ProcessValue(arg)

			if err != nil {
				return err
			}

			cmd.OutOrStdout().Write([]byte(result.(string)))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(valueCmd)
}
