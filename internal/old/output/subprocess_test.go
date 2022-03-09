package output_test

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/benthosdev/benthos/v4/internal/component/metrics"
	"github.com/benthosdev/benthos/v4/internal/integration"
	"github.com/benthosdev/benthos/v4/internal/log"
	"github.com/benthosdev/benthos/v4/internal/manager/mock"
	"github.com/benthosdev/benthos/v4/internal/message"
	"github.com/benthosdev/benthos/v4/internal/old/output"
)

func testProgram(t *testing.T, program string) string {
	t.Helper()

	dir := t.TempDir()

	pathStr := path.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(pathStr, []byte(program), 0o666))

	return pathStr
}

func sendMsg(t *testing.T, msg string, tChan chan message.Transaction) {
	t.Helper()

	m := message.QuickBatch(nil)
	m.Append(message.NewPart([]byte(msg)))

	resChan := make(chan error)

	select {
	case tChan <- message.NewTransaction(m, resChan):
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	select {
	case res := <-resChan:
		require.NotNil(t, res)
		require.NoError(t, res)
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestSubprocessBasic(t *testing.T) {
	integration.CheckSkip(t)

	t.Parallel()

	dir := t.TempDir()

	filePath := testProgram(t, fmt.Sprintf(`package main

import (
	"fmt"
	"bufio"
	"os"
	"strings"
	"bytes"
)

func main() {
	var buf bytes.Buffer

	target := "%v/output.txt"

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fmt.Fprintln(&buf, strings.ToUpper(scanner.Text()))
	}

	if err := scanner.Err(); err != nil {
		panic(err)
	}

	if err := os.WriteFile(target, buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
}
`, dir))

	conf := output.NewConfig()
	conf.Type = output.TypeSubprocess
	conf.Subprocess.Name = "go"
	conf.Subprocess.Args = []string{"run", filePath}

	o, err := output.New(conf, mock.NewManager(), log.Noop(), metrics.Noop())
	require.NoError(t, err)

	tranChan := make(chan message.Transaction)
	require.NoError(t, o.Consume(tranChan))

	sendMsg(t, "foo", tranChan)
	sendMsg(t, "bar", tranChan)
	sendMsg(t, "baz", tranChan)

	o.CloseAsync()
	o.CloseAsync() // No panic on double close

	require.NoError(t, o.WaitForClose(time.Second))

	assert.Eventually(t, func() bool {
		resBytes, err := os.ReadFile(path.Join(dir, "output.txt"))
		if err != nil {
			return false
		}
		return string(resBytes) == "FOO\nBAR\nBAZ\n"
	}, time.Second, time.Millisecond*100)
}

func TestSubprocessEarlyExit(t *testing.T) {
	integration.CheckSkip(t)

	t.Parallel()

	dir := t.TempDir()

	filePath := testProgram(t, fmt.Sprintf(`package main

import (
	"fmt"
	"bufio"
	"os"
	"strings"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		f, err := os.OpenFile("%v/"+scanner.Text()+".txt", os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(f, strings.ToUpper(scanner.Text()))
		f.Sync()
	} else if err := scanner.Err(); err != nil {
		panic(err)
	}
}
`, dir))

	conf := output.NewConfig()
	conf.Type = output.TypeSubprocess
	conf.Subprocess.Name = "go"
	conf.Subprocess.Args = []string{"run", filePath}

	o, err := output.New(conf, mock.NewManager(), log.Noop(), metrics.Noop())
	require.NoError(t, err)

	tranChan := make(chan message.Transaction)
	require.NoError(t, o.Consume(tranChan))

	sendMsg(t, "foo", tranChan)

	assert.Eventually(t, func() bool {
		sendMsg(t, "bar", tranChan)

		resBytes, err := os.ReadFile(path.Join(dir, "bar.txt"))
		if err != nil {
			return false
		}
		return string(resBytes) == "BAR\n"
	}, time.Second, time.Millisecond*100)

	assert.Eventually(t, func() bool {
		sendMsg(t, "baz", tranChan)

		resBytes, err := os.ReadFile(path.Join(dir, "baz.txt"))
		if err != nil {
			return false
		}
		return string(resBytes) == "BAZ\n"
	}, time.Second, time.Millisecond*100)

	o.CloseAsync()
	require.NoError(t, o.WaitForClose(time.Second))

	resBytes, err := os.ReadFile(path.Join(dir, "foo.txt"))
	require.NoError(t, err)
	assert.Equal(t, "FOO\n", string(resBytes))

	resBytes, err = os.ReadFile(path.Join(dir, "bar.txt"))
	require.NoError(t, err)
	assert.Equal(t, "BAR\n", string(resBytes))

	resBytes, err = os.ReadFile(path.Join(dir, "baz.txt"))
	require.NoError(t, err)
	assert.Equal(t, "BAZ\n", string(resBytes))
}
