package ui

import (
	"bufio"
	"strings"
	"testing"
)

func TestStage4InvalidStreamDrainsBeforeWait(t *testing.T) {
	wait := make(chan error, 1)
	model := Model{
		batchEventScanner: bufio.NewScanner(strings.NewReader("not-json\nlate-line\n")),
		batchEventWait:    wait,
		batchStarted:      true,
		batchID:           "batch",
	}
	msg, ok := readBatchEventCmd(&model)().(authEventMsg)
	if !ok || msg.err == nil {
		t.Fatal("expected invalid event error")
	}
	model.batchStreamErr = msg.err
	model.batchDraining = true
	wait <- nil
	result, ok := readBatchEventCmd(&model)().(tokenRenewedMsg)
	if !ok || result.err == nil {
		t.Fatal("expected drained stream to report the original protocol error")
	}
}
