package input

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ibatch "github.com/benthosdev/benthos/v4/internal/batch"
	"github.com/benthosdev/benthos/v4/internal/batch/policy"
	"github.com/benthosdev/benthos/v4/internal/component/metrics"
	"github.com/benthosdev/benthos/v4/internal/log"
	"github.com/benthosdev/benthos/v4/internal/manager/mock"
	"github.com/benthosdev/benthos/v4/internal/message"
)

func TestBatcherStandard(t *testing.T) {
	mockInput := &mockInput{
		ts: make(chan message.Transaction),
	}

	batchConf := policy.NewConfig()
	batchConf.Count = 3

	batchPol, err := policy.New(batchConf, mock.NewManager())
	if err != nil {
		t.Fatal(err)
	}

	batcher := NewBatcher(batchPol, mockInput, log.Noop(), metrics.Noop())

	testMsgs := []string{}
	testResChans := []chan error{}
	for i := 0; i < 8; i++ {
		testMsgs = append(testMsgs, fmt.Sprintf("test%v", i))
		testResChans = append(testResChans, make(chan error))
	}

	resErrs := []error{}
	doneWritesChan := make(chan struct{})
	doneReadsChan := make(chan struct{})
	go func() {
		for i, m := range testMsgs {
			mockInput.ts <- message.NewTransaction(message.QuickBatch([][]byte{[]byte(m)}), testResChans[i])
		}
		close(doneWritesChan)
		for _, rChan := range testResChans {
			resErrs = append(resErrs, (<-rChan))
		}
		close(doneReadsChan)
	}()

	resChans := []chan<- error{}

	var tran message.Transaction
	select {
	case tran = <-batcher.TransactionChan():
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
	resChans = append(resChans, tran.ResponseChan)

	if exp, act := 3, tran.Payload.Len(); exp != act {
		t.Errorf("Wrong batch size: %v != %v", act, exp)
	}
	_ = tran.Payload.Iter(func(i int, part *message.Part) error {
		if exp, act := fmt.Sprintf("test%v", i), string(part.Get()); exp != act {
			t.Errorf("Unexpected message part: %v != %v", act, exp)
		}
		return nil
	})

	select {
	case tran = <-batcher.TransactionChan():
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
	resChans = append(resChans, tran.ResponseChan)

	if exp, act := 3, tran.Payload.Len(); exp != act {
		t.Errorf("Wrong batch size: %v != %v", act, exp)
	}
	_ = tran.Payload.Iter(func(i int, part *message.Part) error {
		if exp, act := fmt.Sprintf("test%v", i+3), string(part.Get()); exp != act {
			t.Errorf("Unexpected message part: %v != %v", act, exp)
		}
		return nil
	})

	select {
	case <-batcher.TransactionChan():
		t.Error("Unexpected batch received")
	default:
	}

	select {
	case <-doneWritesChan:
	case <-time.After(time.Second):
		t.Error("timed out")
	}
	batcher.CloseAsync()

	select {
	case tran = <-batcher.TransactionChan():
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
	resChans = append(resChans, tran.ResponseChan)

	if exp, act := 2, tran.Payload.Len(); exp != act {
		t.Errorf("Wrong batch size: %v != %v", act, exp)
	}
	_ = tran.Payload.Iter(func(i int, part *message.Part) error {
		if exp, act := fmt.Sprintf("test%v", i+6), string(part.Get()); exp != act {
			t.Errorf("Unexpected message part: %v != %v", act, exp)
		}
		return nil
	})

	for i, rChan := range resChans {
		select {
		case rChan <- fmt.Errorf("testerr%v", i):
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}

	select {
	case <-doneReadsChan:
	case <-time.After(time.Second):
		t.Error("timed out")
	}

	for i, err := range resErrs {
		exp := "testerr0"
		if i >= 3 {
			exp = "testerr1"
		}
		if i >= 6 {
			exp = "testerr2"
		}
		if act := err.Error(); exp != act {
			t.Errorf("Unexpected error returned: %v != %v", act, exp)
		}
	}

	if err := batcher.WaitForClose(time.Second); err != nil {
		t.Error(err)
	}
}

func TestBatcherErrorTracking(t *testing.T) {
	mockInput := &mockInput{
		ts: make(chan message.Transaction),
	}

	batchConf := policy.NewConfig()
	batchConf.Count = 3

	batchPol, err := policy.New(batchConf, mock.NewManager())
	require.NoError(t, err)

	batcher := NewBatcher(batchPol, mockInput, log.Noop(), metrics.Noop())

	testMsgs := []string{}
	testResChans := []chan error{}
	for i := 0; i < 3; i++ {
		testMsgs = append(testMsgs, fmt.Sprintf("test%v", i))
		testResChans = append(testResChans, make(chan error))
	}

	resErrs := []error{}
	doneReadsChan := make(chan struct{})
	go func() {
		for i, m := range testMsgs {
			mockInput.ts <- message.NewTransaction(message.QuickBatch([][]byte{[]byte(m)}), testResChans[i])
		}
		for _, rChan := range testResChans {
			resErrs = append(resErrs, (<-rChan))
		}
		close(doneReadsChan)
	}()

	var tran message.Transaction
	select {
	case tran = <-batcher.TransactionChan():
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	assert.Equal(t, 3, tran.Payload.Len())
	_ = tran.Payload.Iter(func(i int, part *message.Part) error {
		assert.Equal(t, fmt.Sprintf("test%v", i), string(part.Get()))
		return nil
	})

	batchErr := ibatch.NewError(tran.Payload, errors.New("ignore this"))
	batchErr.Failed(1, errors.New("message specific error"))
	select {
	case tran.ResponseChan <- batchErr:
	case <-time.After(time.Second * 5):
		t.Fatal("timed out")
	}

	select {
	case <-doneReadsChan:
	case <-time.After(time.Second * 5):
		t.Fatal("timed out")
	}

	require.Len(t, resErrs, 3)
	assert.Nil(t, resErrs[0])
	assert.EqualError(t, resErrs[1], "message specific error")
	assert.Nil(t, resErrs[2])

	mockInput.CloseAsync()
	require.NoError(t, batcher.WaitForClose(time.Second))
}

func TestBatcherTiming(t *testing.T) {
	mockInput := &mockInput{
		ts: make(chan message.Transaction),
	}

	batchConf := policy.NewConfig()
	batchConf.Count = 0
	batchConf.Period = "1ms"

	batchPol, err := policy.New(batchConf, mock.NewManager())
	if err != nil {
		t.Fatal(err)
	}

	batcher := NewBatcher(batchPol, mockInput, log.Noop(), metrics.Noop())

	resChan := make(chan error)
	select {
	case mockInput.ts <- message.NewTransaction(message.QuickBatch([][]byte{[]byte("foo1")}), resChan):
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	var tran message.Transaction
	select {
	case tran = <-batcher.TransactionChan():
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	if exp, act := 1, tran.Payload.Len(); exp != act {
		t.Errorf("Wrong batch size: %v != %v", act, exp)
	}
	if exp, act := "foo1", string(tran.Payload.Get(0).Get()); exp != act {
		t.Errorf("Unexpected message part: %v != %v", act, exp)
	}

	errSend := errors.New("this is a test error")
	select {
	case tran.ResponseChan <- errSend:
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
	select {
	case err := <-resChan:
		if err != errSend {
			t.Errorf("Unexpected error: %v != %v", err, errSend)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	select {
	case mockInput.ts <- message.NewTransaction(message.QuickBatch([][]byte{[]byte("foo2")}), resChan):
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	select {
	case tran = <-batcher.TransactionChan():
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	if exp, act := 1, tran.Payload.Len(); exp != act {
		t.Errorf("Wrong batch size: %v != %v", act, exp)
	}
	if exp, act := "foo2", string(tran.Payload.Get(0).Get()); exp != act {
		t.Errorf("Unexpected message part: %v != %v", act, exp)
	}

	batcher.CloseAsync()

	select {
	case tran.ResponseChan <- errSend:
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
	select {
	case err := <-resChan:
		if err != errSend {
			t.Errorf("Unexpected error: %v != %v", err, errSend)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	if err := batcher.WaitForClose(time.Second); err != nil {
		t.Error(err)
	}
}

func TestBatcherFinalFlush(t *testing.T) {
	mockInput := &mockInput{
		ts: make(chan message.Transaction),
	}

	batchConf := policy.NewConfig()
	batchConf.Count = 10

	batchPol, err := policy.New(batchConf, mock.NewManager())
	require.NoError(t, err)

	batcher := NewBatcher(batchPol, mockInput, log.Noop(), metrics.Noop())

	resChan := make(chan error)
	select {
	case mockInput.ts <- message.NewTransaction(message.QuickBatch([][]byte{[]byte("foo1")}), resChan):
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	mockInput.CloseAsync()

	var tran message.Transaction
	select {
	case tran = <-batcher.TransactionChan():
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	if exp, act := 1, tran.Payload.Len(); exp != act {
		t.Errorf("Wrong batch size: %v != %v", act, exp)
	}
	if exp, act := "foo1", string(tran.Payload.Get(0).Get()); exp != act {
		t.Errorf("Unexpected message part: %v != %v", act, exp)
	}

	batcher.CloseAsync()

	select {
	case tran.ResponseChan <- nil:
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	if err := batcher.WaitForClose(time.Second); err != nil {
		t.Error(err)
	}
}
