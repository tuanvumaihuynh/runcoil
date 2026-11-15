package runcoil

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	runcoilv1 "github.com/runcoil/runcoil/proto/gen/go/runcoil/v1"
	"github.com/runcoil/runcoil/sdks/go/internal/runstate"
)

func TestCutMessage(t *testing.T) {
	assert.Equal(t, "card declined", cutMessage("card declined"))

	long := strings.Repeat("x", maxMessageBytes+10)
	assert.Equal(t, long[:maxMessageBytes], cutMessage(long))

	cut := cutMessage(strings.Repeat("x", maxMessageBytes-1) + "\u20ac")
	assert.Len(t, cut, maxMessageBytes-1)
	assert.True(t, utf8.ValidString(cut))

	assert.Equal(t, "bad\uFFFDbytes", cutMessage("bad\xff\xfebytes"))
}

func TestErrorMessagesAreCutBeforeTheyAreSent(t *testing.T) {
	long := strings.Repeat("x", maxMessageBytes+1)

	run := runResultToProto(nil, errors.New(long))
	assert.Len(t, run.GetError().GetMessage(), maxMessageBytes)

	stepErr := runResultToProto(nil, &runstate.StepError{StepID: "bad\xffid", Message: "declined"})
	assert.Equal(t, "bad\uFFFDid", stepErr.GetError().GetStepId())

	step := stepReportToProto(nil, runstate.StepReport{StepID: "charge", Failure: &runstate.Failure{Kind: runstate.KindError, Message: long}})
	assert.Len(t, step.GetError().GetMessage(), maxMessageBytes)
}

func TestOutputTooLargeKinds(t *testing.T) {
	assert.Equal(t, runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_TOO_LARGE, errorKindToProto(runstate.KindOutputTooLarge))
	assert.Equal(t, runstate.KindOutputTooLarge, errorKindFromProto(runcoilv1.ErrorKind_ERROR_KIND_OUTPUT_TOO_LARGE))
	assert.Equal(t, runstate.KindOutputTooLarge, failureKindFromProto(runcoilv1.Failure_CODE_OUTPUT_TOO_LARGE))
}
