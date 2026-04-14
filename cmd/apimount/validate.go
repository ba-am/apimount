package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/apimount/apimount/internal/core/spec"
)

var validateCmd = &cobra.Command{
	Use:   "validate [spec]",
	Short: "Validate that a spec can be parsed and show stats",
	RunE: func(cmd *cobra.Command, args []string) error {
		specPath := v.GetString("spec")
		if specPath == "" && len(args) > 0 {
			specPath = args[0]
		}
		if specPath == "" {
			return fmt.Errorf("--spec or positional argument required")
		}
		data, err := spec.LoadSpec(specPath)
		if err != nil {
			return err
		}
		ps, err := spec.Parse(data, specPath)
		if err != nil {
			return err
		}

		methods := map[string]int{}
		for _, op := range ps.Operations {
			methods[op.Method]++
		}

		fmt.Printf("✓ Valid OpenAPI spec\n")
		fmt.Printf("  Title:      %s\n", ps.Title)
		fmt.Printf("  Version:    %s\n", ps.Version)
		fmt.Printf("  Base URL:   %s\n", ps.BaseURL)
		fmt.Printf("  Operations: %d total\n", len(ps.Operations))
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			if n := methods[m]; n > 0 {
				fmt.Printf("    %-8s %d\n", m, n)
			}
		}
		fmt.Printf("  Auth schemes: %d\n", len(ps.AuthSchemes))
		for _, s := range ps.AuthSchemes {
			fmt.Printf("    %s (%s)\n", s.Name, s.Type)
		}
		return nil
	},
}
