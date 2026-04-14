package main

import (
	"fmt"
	"os"
	"os/exec"
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
		fmt.Println("FUSE frontend:")
		fusePath := findFUSEBinary()
		check("FUSE userspace", fusePath != "", fuseDetail(fusePath))

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

func findFUSEBinary() string {
	candidates := []string{"fusermount3", "fusermount"}
	if runtime.GOOS == "darwin" {
		candidates = []string{"fusermount", "mount_macfuse", "mount_osxfuse"}
	}
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	// On macOS the userspace bin may not be on PATH — check known install paths.
	if runtime.GOOS == "darwin" {
		for _, p := range []string{"/usr/local/bin/macfuse", "/Library/Filesystems/macfuse.fs"} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func fuseDetail(path string) string {
	if path == "" {
		if runtime.GOOS == "darwin" {
			return "macFUSE not found; install from https://osxfuse.github.io or `brew install --cask macfuse`"
		}
		return "fusermount/fusermount3 not found; install fuse3 via your package manager"
	}
	return path
}
