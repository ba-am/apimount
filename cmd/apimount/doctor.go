package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	corespec "github.com/apimount/apimount/internal/core/spec"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the local environment for apimount prerequisites",
	RunE: func(cmd *cobra.Command, args []string) error {
		fails := 0
		check := func(name string, ok bool, detail string) {
			if ok {
				fmt.Printf("  ✓ %s — %s\n", name, detail)
				return
			}
			fails++
			fmt.Printf("  ✗ %s — %s\n", name, detail)
		}

		fmt.Println("apimount doctor")
		fmt.Printf("  os / arch:         %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Printf("  go runtime:        %s\n", runtime.Version())

		fmt.Println()
		fmt.Println("Spec access:")
		specPath := v.GetString("spec")
		if specPath == "" {
			fmt.Println("  − spec          — no --spec provided, skipping reachability check")
		} else {
			data, err := corespec.LoadSpec(specPath)
			if err != nil {
				check("spec reachable", false, err.Error())
			} else {
				ps, err := corespec.Parse(data, specPath)
				if err != nil {
					check("spec parses", false, err.Error())
				} else {
					check("spec parses", true, fmt.Sprintf("%s (%d operations)", ps.Title, len(ps.Operations)))
				}
			}
		}

		fmt.Println()
		fmt.Println("Config file:")
		cfgUsed := v.ConfigFileUsed()
		if cfgUsed == "" {
			fmt.Println("  − config file    — none found (optional)")
		} else {
			fmt.Printf("  ✓ config file    — %s\n", cfgUsed)
		}

		fmt.Println()
		if fails == 0 {
			fmt.Println("✓ all checks passed")
			return nil
		}
		return fmt.Errorf("%d check(s) failed", fails)
	},
}
