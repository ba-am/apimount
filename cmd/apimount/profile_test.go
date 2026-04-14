package main

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfile_List_Empty(t *testing.T) {
	prev := v
	v = viper.New()
	defer func() { v = prev }()

	out := captureStdout(t, func() {
		require.NoError(t, profileListCmd.RunE(profileListCmd, nil))
	})
	assert.Contains(t, out, "no profiles defined")
}

func TestProfile_List_MultipleSorted(t *testing.T) {
	prev := v
	fresh := viper.New()
	fresh.Set("profiles", map[string]interface{}{
		"zeta":  map[string]interface{}{"spec": "z.yaml"},
		"alpha": map[string]interface{}{"spec": "a.yaml"},
	})
	v = fresh
	defer func() { v = prev }()

	out := captureStdout(t, func() {
		require.NoError(t, profileListCmd.RunE(profileListCmd, nil))
	})
	// alpha must come before zeta.
	ai := indexOf(out, "alpha")
	zi := indexOf(out, "zeta")
	require.GreaterOrEqual(t, ai, 0, "alpha missing: %q", out)
	require.Greater(t, zi, ai, "zeta not listed after alpha: %q", out)
}

func TestProfile_Show_MissingProfile(t *testing.T) {
	prev := v
	v = viper.New()
	defer func() { v = prev }()

	err := profileShowCmd.RunE(profileShowCmd, []string{"nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no profile named")
}

func TestRedactSecret(t *testing.T) {
	assert.Equal(t, "****", redact("abc"))
	assert.Equal(t, "****", redact("abcdef"))
	assert.NotEqual(t, "ghp_abcdefghijklmn", redact("ghp_abcdefghijklmn"))
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
