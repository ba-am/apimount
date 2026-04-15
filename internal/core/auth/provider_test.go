package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider records that it ran and optionally fails.
type fakeProvider struct {
	name    string
	key     string
	val     string
	fail    error
	called  int
	seenHdr map[string]string
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Apply(_ context.Context, tgt *ApplyTarget) error {
	f.called++
	f.seenHdr = tgt.Headers
	if f.fail != nil {
		return f.fail
	}
	if tgt.Headers == nil {
		tgt.Headers = make(map[string]string)
	}
	tgt.Headers[f.key] = f.val
	return nil
}

func TestChain_RunsInOrder(t *testing.T) {
	a := &fakeProvider{name: "a", key: "X-A", val: "1"}
	b := &fakeProvider{name: "b", key: "X-B", val: "2"}
	c := NewChain(a, b)

	tgt := &ApplyTarget{Headers: map[string]string{}}
	require.NoError(t, c.Apply(context.Background(), tgt))

	assert.Equal(t, 1, a.called)
	assert.Equal(t, 1, b.called)
	assert.Equal(t, "1", tgt.Headers["X-A"])
	assert.Equal(t, "2", tgt.Headers["X-B"])
}

func TestChain_ShortCircuitsOnError(t *testing.T) {
	bad := &fakeProvider{name: "bad", fail: errors.New("boom")}
	good := &fakeProvider{name: "good", key: "X-G", val: "ok"}
	c := NewChain(bad, good)

	tgt := &ApplyTarget{Headers: map[string]string{}}
	err := c.Apply(context.Background(), tgt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.Equal(t, 1, bad.called)
	assert.Equal(t, 0, good.called, "provider after failure must not run")
	assert.Empty(t, tgt.Headers["X-G"])
}

func TestChain_NilSafe(t *testing.T) {
	var c *Chain
	require.NoError(t, c.Apply(context.Background(), &ApplyTarget{}))
	assert.Equal(t, 0, c.Len())

	empty := NewChain()
	require.NoError(t, empty.Apply(context.Background(), &ApplyTarget{}))
	assert.Equal(t, 0, empty.Len())
}

func TestChain_DropsNilProviders(t *testing.T) {
	a := &fakeProvider{name: "a", key: "X-A", val: "1"}
	c := NewChain(nil, a, nil)
	assert.Equal(t, 1, c.Len())

	tgt := &ApplyTarget{Headers: map[string]string{}}
	require.NoError(t, c.Apply(context.Background(), tgt))
	assert.Equal(t, "1", tgt.Headers["X-A"])
}

func TestStaticProvider_WrapsInjector(t *testing.T) {
	inj := NewInjector(&Config{Bearer: "tok"}, nil)
	p := NewStaticProvider(inj)
	tgt := &ApplyTarget{Headers: map[string]string{}, QueryParams: map[string]string{}}

	require.NoError(t, p.Apply(context.Background(), tgt))
	assert.Equal(t, "Bearer tok", tgt.Headers["Authorization"])
}
