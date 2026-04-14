package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/apimount/apimount/internal/core/plan"
	"github.com/apimount/apimount/internal/core/spec"
)

var treeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Print the filesystem tree for a spec",
	RunE: func(cmd *cobra.Command, args []string) error {
		specPath := v.GetString("spec")
		if specPath == "" {
			return fmt.Errorf("--spec is required")
		}
		groupBy, _ := cmd.Flags().GetString("group-by")
		if groupBy == "" {
			groupBy = defaultGroupBy
		}

		data, err := spec.LoadSpec(specPath)
		if err != nil {
			return err
		}
		ps, err := spec.Parse(data, specPath)
		if err != nil {
			return err
		}
		fmt.Print(plan.PrintTree(plan.BuildTree(ps, groupBy)))
		return nil
	},
}

func init() {
	treeCmd.Flags().String("group-by", defaultGroupBy, "tree grouping: tags|path|flat")
}
