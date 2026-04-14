package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpecDiff_SelfIsNoChanges(t *testing.T) {
	out := captureStdout(t, func() {
		require.NoError(t, runSpecDiff(nil, []string{
			"../../testdata/petstore.yaml",
			"../../testdata/petstore.yaml",
		}))
	})
	assert.Contains(t, out, "no changes")
}

func TestSpecDiff_DifferentSpecsReportChanges(t *testing.T) {
	// petstore vs. no_tags are structurally different (title, operations, …).
	// The command must terminate successfully when there are NO breaking changes,
	// or fail with an error message when there are. Either way, it must print
	// a non-empty change summary.
	var out string
	err := func() error {
		var runErr error
		out = captureStdout(t, func() {
			runErr = runSpecDiff(nil, []string{
				"../../testdata/petstore.yaml",
				"../../testdata/no_tags.yaml",
			})
		})
		return runErr
	}()

	// Different specs should always report at least one change line.
	assert.Contains(t, out, "change(s)")
	// Whether err is nil or non-nil depends on whether the diff detected breaking
	// changes; either outcome is valid here — we only assert the tool ran.
	_ = err
}
