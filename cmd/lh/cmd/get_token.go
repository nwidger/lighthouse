package cmd

import (
	"github.com/nwidger/lighthouse/tokens"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// tokenCmd represents the token command
var tokenCmd = &cobra.Command{
	Use:   "token [token-str]",
	Short: "Get information about an API token",
	Run: func(cmd *cobra.Command, args []string) {
		tokens := tokens.NewService(service)
		tk := viper.GetString("token")
		if len(args) == 0 && len(tk) == 0 {
			FatalUsage(cmd, "must supply token")
		}
		t, err := tokens.Get(tk)
		if err != nil {
			FatalUsage(cmd, err)
		}
		JSON(t)
	},
}

func init() {
	getCmd.AddCommand(tokenCmd)
}
