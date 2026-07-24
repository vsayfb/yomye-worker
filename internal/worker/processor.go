package worker

import (
	"context"
)

type Processor interface {
	Process(context.Context, Message) error
	CategorizationSQSQueue() string
}
