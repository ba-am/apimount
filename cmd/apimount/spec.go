package main

import (
	"fmt"

	"github.com/pb33f/libopenapi"
	wcmodel "github.com/pb33f/libopenapi/what-changed/model"
	"github.com/spf13/cobra"

	corespec "github.com/apimount/apimount/internal/core/spec"
)

var specCmd = &cobra.Command{
	Use:   "spec",
	Short: "Spec utilities (diff, stats, …)",
}

var specDiffCmd = &cobra.Command{
	Use:   "diff OLD NEW",
	Short: "Diff two OpenAPI specs (uses libopenapi what-changed)",
	Args:  cobra.ExactArgs(2),
	RunE:  runSpecDiff,
}

func init() {
	specCmd.AddCommand(specDiffCmd)
}

func runSpecDiff(cmd *cobra.Command, args []string) error {
	oldPath, newPath := args[0], args[1]

	oldDoc, err := loadLibopenapiDoc(oldPath)
	if err != nil {
		return fmt.Errorf("load old spec: %w", err)
	}
	newDoc, err := loadLibopenapiDoc(newPath)
	if err != nil {
		return fmt.Errorf("load new spec: %w", err)
	}

	changes, err := libopenapi.CompareDocuments(oldDoc, newDoc)
	if err != nil && changes == nil {
		return fmt.Errorf("compare documents: %w", err)
	}
	if changes == nil {
		fmt.Println("no changes")
		return nil
	}

	total := changes.TotalChanges()
	breaking := changes.TotalBreakingChanges()
	fmt.Printf("%d total change(s), %d breaking\n", total, breaking)
	if total == 0 {
		return nil
	}

	for _, c := range changes.GetAllChanges() {
		marker := " "
		if c.Breaking {
			marker = "!"
		}
		fmt.Printf("  %s %-9s %-32s %q → %q\n",
			marker,
			changeTypeName(c.ChangeType),
			truncate(c.Property, 32),
			c.Original,
			c.New,
		)
	}
	if breaking > 0 {
		return fmt.Errorf("%d breaking change(s)", breaking)
	}
	return nil
}

func loadLibopenapiDoc(path string) (libopenapi.Document, error) {
	data, err := corespec.LoadSpec(path)
	if err != nil {
		return nil, err
	}
	return libopenapi.NewDocument(data)
}

func changeTypeName(t int) string {
	switch t {
	case wcmodel.Modified:
		return "modified"
	case wcmodel.PropertyAdded:
		return "added"
	case wcmodel.PropertyRemoved:
		return "removed"
	case wcmodel.ObjectAdded:
		return "obj+"
	case wcmodel.ObjectRemoved:
		return "obj-"
	default:
		return "change"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
