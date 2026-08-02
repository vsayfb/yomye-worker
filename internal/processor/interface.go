package processor

import (
	"context"
)

type Message struct {
	ID   string
	Type string
	Body string
}

type Processor interface {
	Process(context.Context, Message) error
}
