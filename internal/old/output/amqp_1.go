package output

import (
	"github.com/benthosdev/benthos/v4/internal/component/metrics"
	"github.com/benthosdev/benthos/v4/internal/component/output"
	"github.com/benthosdev/benthos/v4/internal/docs"
	"github.com/benthosdev/benthos/v4/internal/impl/amqp1"
	"github.com/benthosdev/benthos/v4/internal/interop"
	"github.com/benthosdev/benthos/v4/internal/log"
	"github.com/benthosdev/benthos/v4/internal/metadata"
	"github.com/benthosdev/benthos/v4/internal/old/output/writer"
	"github.com/benthosdev/benthos/v4/internal/tls"
)

//------------------------------------------------------------------------------

func init() {
	Constructors[TypeAMQP1] = TypeSpec{
		constructor: fromSimpleConstructor(NewAMQP1),
		Status:      docs.StatusBeta,
		Summary: `
Sends messages to an AMQP (1.0) server.`,
		Description: `
### Metadata

Message metadata is added to each AMQP message as string annotations. In order to control which metadata keys are added use the ` + "`metadata`" + ` config field.`,
		Async: true,
		FieldSpecs: docs.FieldSpecs{
			docs.FieldCommon("url",
				"A URL to connect to.",
				"amqp://localhost:5672/",
				"amqps://guest:guest@localhost:5672/",
			),
			docs.FieldCommon("target_address", "The target address to write to.", "/foo", "queue:/bar", "topic:/baz"),
			docs.FieldCommon("max_in_flight", "The maximum number of messages to have in flight at a given time. Increase this to improve throughput."),
			tls.FieldSpec(),
			amqp1.SASLFieldSpec(),
			docs.FieldCommon("metadata", "Specify criteria for which metadata values are attached to messages as headers.").WithChildren(metadata.ExcludeFilterFields()...),
		},
		Categories: []Category{
			CategoryServices,
		},
	}
}

//------------------------------------------------------------------------------

// NewAMQP1 creates a new AMQP output type.
func NewAMQP1(conf Config, mgr interop.Manager, log log.Modular, stats metrics.Type) (output.Streamed, error) {
	a, err := writer.NewAMQP1(conf.AMQP1, log, stats)
	if err != nil {
		return nil, err
	}
	w, err := NewAsyncWriter(
		TypeAMQP1, conf.AMQP1.MaxInFlight, a, log, stats,
	)
	if err != nil {
		return nil, err
	}
	return OnlySinglePayloads(w), nil
}

//------------------------------------------------------------------------------
