package core_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/apimount/apimount/internal/core"
)

func TestError_KindString(t *testing.T) {
	assert.Equal(t, "validation", core.KindValidation.String())
	assert.Equal(t, "auth", core.KindAuth.String())
	assert.Equal(t, "network", core.KindNetwork.String())
	assert.Equal(t, "upstream", core.KindUpstream.String())
	assert.Equal(t, "policy", core.KindPolicy.String())
	assert.Equal(t, "internal", core.KindInternal.String())
}

func TestError_ErrorString(t *testing.T) {
	e := core.New(core.KindUpstream, "upstream.not_found", "resource not found")
	assert.Contains(t, e.Error(), "upstream.not_found")
	assert.Contains(t, e.Error(), "resource not found")
}

func TestError_Wrap(t *testing.T) {
	cause := errors.New("connection refused")
	e := core.Wrap(core.KindNetwork, "network.connect", "could not connect", cause)
	assert.ErrorIs(t, e, cause)
	assert.Contains(t, e.Error(), "connection refused")
}

func TestError_Fields(t *testing.T) {
	e := &core.Error{
		Kind:    core.KindUpstream,
		Code:    "upstream.forbidden",
		Message: "forbidden",
		OpID:    "getPet",
		Status:  403,
	}
	assert.Equal(t, core.KindUpstream, e.Kind)
	assert.Equal(t, "getPet", e.OpID)
	assert.Equal(t, 403, e.Status)
}
