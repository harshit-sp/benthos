package manager

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/benthosdev/benthos/v4/internal/bundle"
	"github.com/benthosdev/benthos/v4/internal/component"
	"github.com/benthosdev/benthos/v4/internal/component/metrics"
	"github.com/benthosdev/benthos/v4/internal/component/processor"
	"github.com/benthosdev/benthos/v4/internal/log"
	"github.com/benthosdev/benthos/v4/internal/stream"
)

// StreamStatus tracks a stream along with information regarding its internals.
type StreamStatus struct {
	stoppedAfter int64
	config       stream.Config
	strm         *stream.Type
	logger       log.Modular
	metrics      *metrics.Local
	createdAt    time.Time
}

// NewStreamStatus creates a new StreamStatus.
func NewStreamStatus(
	conf stream.Config,
	strm *stream.Type,
	logger log.Modular,
	stats *metrics.Local,
) *StreamStatus {
	return &StreamStatus{
		config:    conf,
		strm:      strm,
		logger:    logger,
		metrics:   stats,
		createdAt: time.Now(),
	}
}

// IsRunning returns a boolean indicating whether the stream is currently
// running.
func (s *StreamStatus) IsRunning() bool {
	return atomic.LoadInt64(&s.stoppedAfter) == 0
}

// IsReady returns a boolean indicating whether the stream is connected at both
// the input and output level.
func (s *StreamStatus) IsReady() bool {
	return s.strm.IsReady()
}

// Uptime returns a time.Duration indicating the current uptime of the stream.
func (s *StreamStatus) Uptime() time.Duration {
	if stoppedAfter := atomic.LoadInt64(&s.stoppedAfter); stoppedAfter > 0 {
		return time.Duration(stoppedAfter)
	}
	return time.Since(s.createdAt)
}

// Config returns the configuration of the stream.
func (s *StreamStatus) Config() stream.Config {
	return s.config
}

// Metrics returns a metrics aggregator of the stream.
func (s *StreamStatus) Metrics() *metrics.Local {
	return s.metrics
}

// setClosed sets the flag indicating that the stream is closed.
func (s *StreamStatus) setClosed() {
	atomic.SwapInt64(&s.stoppedAfter, int64(time.Since(s.createdAt)))
}

//------------------------------------------------------------------------------

// StreamProcConstructorFunc is a closure type that constructs a processor type
// for new streams, where the id of the stream is provided as an argument.
type StreamProcConstructorFunc func(streamID string) (processor.V1, error)

//------------------------------------------------------------------------------

// Type manages a collection of streams, providing APIs for CRUD operations on
// the streams.
type Type struct {
	closed  bool
	streams map[string]*StreamStatus

	manager    bundle.NewManagement
	apiTimeout time.Duration
	apiEnabled bool

	lock sync.Mutex
}

// New creates a new stream manager.Type.
func New(mgr bundle.NewManagement, opts ...func(*Type)) *Type {
	t := &Type{
		streams:    map[string]*StreamStatus{},
		apiTimeout: time.Second * 5,
		apiEnabled: true,
		manager:    mgr,
	}
	for _, opt := range opts {
		opt(t)
	}
	t.registerEndpoints(t.apiEnabled)
	return t
}

//------------------------------------------------------------------------------

// OptAPIEnabled sets whether the stream manager registers API endpoints for
// CRUD operations on streams. This is enabled by default.
func OptAPIEnabled(b bool) func(*Type) {
	return func(t *Type) {
		t.apiEnabled = b
	}
}

// OptSetAPITimeout sets the default timeout for HTTP API requests.
func OptSetAPITimeout(tout time.Duration) func(*Type) {
	return func(t *Type) {
		t.apiTimeout = tout
	}
}

//------------------------------------------------------------------------------

// Errors specifically returned by a stream manager.
var (
	ErrStreamExists       = errors.New("stream already exists")
	ErrStreamDoesNotExist = errors.New("stream does not exist")
)

//------------------------------------------------------------------------------

// Create attempts to construct and run a new stream under a unique ID. If the
// ID already exists an error is returned.
func (m *Type) Create(id string, conf stream.Config) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.closed {
		return component.ErrTypeClosed
	}

	if _, exists := m.streams[id]; exists {
		return ErrStreamExists
	}

	strmFlatMetrics := metrics.NewLocal()
	sMgr := m.manager.ForStream(id).WithAddedMetrics(strmFlatMetrics).(bundle.NewManagement)

	var wrapper *StreamStatus
	strm, err := stream.New(conf, sMgr, stream.OptOnClose(func() {
		wrapper.setClosed()
	}))
	if err != nil {
		return err
	}

	wrapper = NewStreamStatus(conf, strm, sMgr.Logger(), strmFlatMetrics)
	m.streams[id] = wrapper
	return nil
}

// Read attempts to obtain the status of a managed stream. Returns an error if
// the stream does not exist.
func (m *Type) Read(id string) (*StreamStatus, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.closed {
		return nil, component.ErrTypeClosed
	}

	wrapper, exists := m.streams[id]
	if !exists {
		return nil, ErrStreamDoesNotExist
	}

	return wrapper, nil
}

// Update attempts to stop an existing stream and replace it with a new version
// of the same stream.
func (m *Type) Update(id string, conf stream.Config, timeout time.Duration) error {
	m.lock.Lock()
	wrapper, exists := m.streams[id]
	closed := m.closed
	m.lock.Unlock()

	if closed {
		return component.ErrTypeClosed
	}
	if !exists {
		return ErrStreamDoesNotExist
	}

	if reflect.DeepEqual(wrapper.config, conf) {
		return nil
	}

	if err := m.Delete(id, timeout); err != nil {
		return err
	}
	return m.Create(id, conf)
}

// Delete attempts to stop and remove a stream by its ID. Returns an error if
// the stream was not found, or if clean shutdown fails in the specified period
// of time.
func (m *Type) Delete(id string, timeout time.Duration) error {
	m.lock.Lock()
	if m.closed {
		m.lock.Unlock()
		return component.ErrTypeClosed
	}

	wrapper, exists := m.streams[id]
	m.lock.Unlock()
	if !exists {
		return ErrStreamDoesNotExist
	}

	if err := wrapper.strm.Stop(timeout); err != nil {
		return err
	}

	m.lock.Lock()
	delete(m.streams, id)
	m.lock.Unlock()

	return nil
}

//------------------------------------------------------------------------------

// Stop attempts to gracefully shut down all active streams and close the
// stream manager.
func (m *Type) Stop(timeout time.Duration) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	resultChan := make(chan string)

	for k, v := range m.streams {
		go func(id string, strm *StreamStatus) {
			if err := strm.strm.Stop(timeout); err != nil {
				resultChan <- id
			} else {
				resultChan <- ""
			}
		}(k, v)
	}

	failedStreams := []string{}
	for i := 0; i < len(m.streams); i++ {
		if failedStrm := <-resultChan; len(failedStrm) > 0 {
			failedStreams = append(failedStreams, failedStrm)
		}
	}

	m.streams = map[string]*StreamStatus{}
	m.closed = true

	if len(failedStreams) > 0 {
		return fmt.Errorf("failed to gracefully stop the following streams: %v", failedStreams)
	}
	return nil
}

//------------------------------------------------------------------------------
