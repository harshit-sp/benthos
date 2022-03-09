package pipeline

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/benthosdev/benthos/v4/internal/log"
	"github.com/benthosdev/benthos/v4/internal/manager/mock"
	"github.com/benthosdev/benthos/v4/internal/message"
	"github.com/benthosdev/benthos/v4/internal/old/processor"
)

func TestPoolBasic(t *testing.T) {
	mockProc := &mockMsgProcessor{dropChan: make(chan bool)}
	go func() {
		mockProc.dropChan <- true
	}()

	proc, err := newPoolV2(1, log.Noop(), mockProc)
	require.NoError(t, err)

	tChan, resChan := make(chan message.Transaction), make(chan error)

	require.NoError(t, proc.Consume(tChan))
	assert.Error(t, proc.Consume(tChan))

	msg := message.QuickBatch([][]byte{
		[]byte(`one`),
		[]byte(`two`),
	})

	// First message should be dropped and return immediately
	select {
	case tChan <- message.NewTransaction(msg, resChan):
	case <-time.After(time.Second):
		t.Fatal("Timed out")
	}
	select {
	case _, open := <-proc.TransactionChan():
		if !open {
			t.Fatal("Closed early")
		} else {
			t.Fatal("Message was not dropped")
		}
	case res, open := <-resChan:
		if !open {
			t.Fatal("Closed early")
		}
		if res != errMockProc {
			t.Error(res)
		}
	case <-time.After(time.Second * 5):
		t.Fatal("Timed out")
	}

	// Do not drop next message
	go func() {
		mockProc.dropChan <- false
	}()

	// Send message
	select {
	case tChan <- message.NewTransaction(msg, resChan):
	case <-time.After(time.Second * 5):
		t.Fatal("Timed out")
	}

	var procT message.Transaction
	var open bool

	// Receive new message
	select {
	case procT, open = <-proc.TransactionChan():
		if !open {
			t.Error("Closed early")
		}
		if exp, act := [][]byte{[]byte("foo"), []byte("bar")}, message.GetAllBytes(procT.Payload); !reflect.DeepEqual(exp, act) {
			t.Errorf("Wrong message received: %s != %s", act, exp)
		}
	case <-time.After(time.Second * 5):
		t.Fatal("Timed out")
	}

	// Respond without error
	go func() {
		select {
		case procT.ResponseChan <- nil:
		case <-time.After(time.Second * 5):
			t.Error("Timed out")
		}
	}()

	// Receive response
	select {
	case res, open := <-resChan:
		if !open {
			t.Error("Closed early")
		}
		if res != nil {
			t.Error(res)
		}
	case <-time.After(time.Second * 5):
		t.Fatal("Timed out")
	}

	proc.CloseAsync()
	if err := proc.WaitForClose(time.Second * 5); err != nil {
		t.Error(err)
	}
}

func TestPoolMultiMsgs(t *testing.T) {
	mockProc := &mockMultiMsgProcessor{N: 3}

	proc, err := newPoolV2(1, log.Noop(), mockProc)
	if err != nil {
		t.Fatal(err)
	}

	tChan, resChan := make(chan message.Transaction), make(chan error)
	if err := proc.Consume(tChan); err != nil {
		t.Fatal(err)
	}

	for j := 0; j < 10; j++ {
		expMsgs := map[string]struct{}{}
		for i := 0; i < mockProc.N; i++ {
			expMsgs[fmt.Sprintf("test%v", i)] = struct{}{}
		}

		// Send message
		select {
		case tChan <- message.NewTransaction(message.QuickBatch(nil), resChan):
		case <-time.After(time.Second * 5):
			t.Fatal("Timed out")
		}

		for i := 0; i < mockProc.N; i++ {
			// Receive messages
			var procT message.Transaction
			var open bool
			select {
			case procT, open = <-proc.TransactionChan():
				if !open {
					t.Error("Closed early")
				}
				act := string(procT.Payload.Get(0).Get())
				if _, exists := expMsgs[act]; !exists {
					t.Errorf("Unexpected result: %v", act)
				} else {
					delete(expMsgs, act)
				}
			case <-time.After(time.Second * 5):
				t.Fatal("Timed out")
			}

			// Respond with no error
			select {
			case procT.ResponseChan <- nil:
			case <-time.After(time.Second * 5):
				t.Fatal("Timed out")
			}

		}

		// Receive response
		select {
		case res, open := <-resChan:
			if !open {
				t.Error("Closed early")
			} else if res != nil {
				t.Error(res)
			}
		case <-time.After(time.Second * 5):
			t.Fatal("Timed out")
		}

		if len(expMsgs) != 0 {
			t.Errorf("Expected messages were not received: %v", expMsgs)
		}
	}

	proc.CloseAsync()
	if err := proc.WaitForClose(time.Second * 5); err != nil {
		t.Error(err)
	}
}

func TestPoolMultiThreads(t *testing.T) {
	conf := NewConfig()
	conf.Threads = 2
	conf.Processors = append(conf.Processors, processor.NewConfig())

	proc, err := New(conf, mock.NewManager())
	if err != nil {
		t.Fatal(err)
	}

	tChan, resChan := make(chan message.Transaction), make(chan error)
	if err := proc.Consume(tChan); err != nil {
		t.Fatal(err)
	}

	msg := message.QuickBatch([][]byte{
		[]byte(`one`),
		[]byte(`two`),
	})

	for j := 0; j < conf.Threads; j++ {
		// Send message
		select {
		case tChan <- message.NewTransaction(msg, resChan):
		case <-time.After(time.Second * 5):
			t.Fatal("Timed out")
		}
	}
	for j := 0; j < conf.Threads; j++ {
		// Receive messages
		var procT message.Transaction
		var open bool
		select {
		case procT, open = <-proc.TransactionChan():
			if !open {
				t.Error("Closed early")
			}
			if exp, act := [][]byte{[]byte("one"), []byte("two")}, message.GetAllBytes(procT.Payload); !reflect.DeepEqual(exp, act) {
				t.Errorf("Wrong message received: %s != %s", act, exp)
			}
		case <-time.After(time.Second * 5):
			t.Fatal("Timed out")
		}

		go func(tran message.Transaction) {
			// Respond with no error
			select {
			case tran.ResponseChan <- nil:
			case <-time.After(time.Second * 5):
				t.Error("Timed out")
			}
		}(procT)
	}
	for j := 0; j < conf.Threads; j++ {
		// Receive response
		select {
		case res, open := <-resChan:
			if !open {
				t.Error("Closed early")
			} else if res != nil {
				t.Error(res)
			}
		case <-time.After(time.Second * 5):
			t.Fatal("Timed out")
		}
	}

	proc.CloseAsync()
	if err := proc.WaitForClose(time.Second * 5); err != nil {
		t.Error(err)
	}
}

func TestPoolMultiNaturalClose(t *testing.T) {
	conf := NewConfig()
	conf.Threads = 2
	conf.Processors = append(conf.Processors, processor.NewConfig())

	proc, err := New(conf, mock.NewManager())
	if err != nil {
		t.Fatal(err)
	}

	tChan := make(chan message.Transaction)
	if err := proc.Consume(tChan); err != nil {
		t.Fatal(err)
	}

	close(tChan)

	if err := proc.WaitForClose(time.Second * 5); err != nil {
		t.Error(err)
	}
}
