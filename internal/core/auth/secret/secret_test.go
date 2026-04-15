package secret

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_Env(t *testing.T) {
	t.Setenv("APIMOUNT_TEST_TOKEN", "deadbeef")
	r := NewRegistry()

	got, err := r.Resolve(context.Background(), "env:APIMOUNT_TEST_TOKEN")
	require.NoError(t, err)
	assert.Equal(t, "deadbeef", got)
}

func TestRegistry_EnvMissing(t *testing.T) {
	r := NewRegistry()
	_, err := r.Resolve(context.Background(), "env:APIMOUNT_TEST_NOT_SET_XYZ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not set")
}

func TestRegistry_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(path, []byte("my-secret-token\n"), 0o600))

	r := NewRegistry()
	got, err := r.Resolve(context.Background(), "file:"+path)
	require.NoError(t, err)
	assert.Equal(t, "my-secret-token", got, "trailing newline must be trimmed")
}

func TestRegistry_FileRejectsLoosePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce Unix file modes")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	r := NewRegistry()
	_, err := r.Resolve(context.Background(), "file:"+path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "0600")
}

func TestRegistry_Literal(t *testing.T) {
	r := NewRegistry()
	got, err := r.Resolve(context.Background(), "literal:hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

func TestRegistry_NoScheme_IsLiteral(t *testing.T) {
	r := NewRegistry()
	// Refs without "scheme:" are treated as literals for CLI ergonomics.
	got, err := r.Resolve(context.Background(), "plain-token")
	require.NoError(t, err)
	assert.Equal(t, "plain-token", got)
}

func TestRegistry_UnknownScheme_IsLiteral(t *testing.T) {
	r := NewRegistry()
	// Unknown scheme is returned as-is so we don't accidentally eat a real
	// token that happens to contain a colon (e.g. "basic:user:pass").
	got, err := r.Resolve(context.Background(), "weird:value")
	require.NoError(t, err)
	assert.Equal(t, "weird:value", got)
}

func TestRegistry_EmptyRef(t *testing.T) {
	r := NewRegistry()
	got, err := r.Resolve(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "", got)
}
