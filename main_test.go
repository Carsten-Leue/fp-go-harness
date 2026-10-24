package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/stretchr/testify/assert"
)

func TestExitCode(t *testing.T) {
	assert.Equal(t, exitOK, exitCode()(nil))
	assert.Equal(t, exitCancelled, exitCode()(context.Canceled))
	assert.Equal(t, exitCancelled, exitCode()(fmt.Errorf("request: %w", context.Canceled)))
	assert.Equal(t, exitFailure, exitCode()(&session.MaxIterationsError{MaxIterations: 3}))
	assert.Equal(t, exitFailure, exitCode()(errors.New("boom")))
}
