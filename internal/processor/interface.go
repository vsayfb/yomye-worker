package processor

import (
	"context"
	"errors"
	"fmt"
)

type Message struct {
	ID   string
	Type string
	Body string
}

type Processor interface {
	Process(context.Context, Message) error
}

// PermanentError marks a message-contract failure that retrying cannot fix.
// Infrastructure and dependency failures must remain ordinary errors so SQS
// can redeliver them.
type PermanentError struct {
	err error
}

func (e *PermanentError) Error() string {
	return e.err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.err
}

func Permanent(err error) error {
	if err == nil {
		return nil
	}

	var permanent *PermanentError
	if errors.As(err, &permanent) {
		return err
	}

	return &PermanentError{err: fmt.Errorf("permanent message error: %w", err)}
}

func IsPermanent(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}
