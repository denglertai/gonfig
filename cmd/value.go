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
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, arg := range args {
			result, err := value.ProcessValue(arg)

			if err != nil {
				return err
			}

			cmd.Print(result)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(valueCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// valueCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// valueCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
