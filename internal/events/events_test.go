package events_test

import (
	"errors"
	"testing"

	"github.com/jorm/internal/events"
)

// TestSinkInterface_HasStageFailed is a compile-time check that events.Sink includes
// StageFailed and that PrintSink implements it. Covers AC4.
//
// This test will not compile until StageFailed is added to the Sink interface and
// implemented on PrintSink.
func TestSinkInterface_HasStageFailed(t *testing.T) {
	var s events.Sink = &events.PrintSink{}
	// Calling through the interface ensures both the method is declared on Sink
	// AND PrintSink satisfies it.
	s.StageFailed(0, "Planning", errors.New("test error"))
}

// TestSinkInterface_HasStageRoundStarted is a compile-time check that events.Sink includes
// StageRoundStarted and that PrintSink implements it. Covers AC5.
//
// This test will not compile until StageRoundStarted is added to the Sink interface and
// implemented on PrintSink.
func TestSinkInterface_HasStageRoundStarted(t *testing.T) {
	var s events.Sink = &events.PrintSink{}
	s.StageRoundStarted(0, 1)
}

// TestPrintSink_StageFailed_DoesNotPanic verifies PrintSink.StageFailed is safe to call
// with any argument combination (including nil error).
func TestPrintSink_StageFailed_DoesNotPanic(t *testing.T) {
	s := &events.PrintSink{}
	s.StageFailed(0, "Planning", errors.New("some error"))
	s.StageFailed(1, "PR Review", nil)
}

// TestPrintSink_StageRoundStarted_DoesNotPanic verifies PrintSink.StageRoundStarted is safe.
func TestPrintSink_StageRoundStarted_DoesNotPanic(t *testing.T) {
	s := &events.PrintSink{}
	s.StageRoundStarted(0, 1)
	s.StageRoundStarted(2, 3)
}
