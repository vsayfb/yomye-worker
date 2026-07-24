package tracing

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"go.opentelemetry.io/otel"
)

type sqsCarrier struct {
	attrs map[string]types.MessageAttributeValue
}

func (c sqsCarrier) Get(key string) string {
	if v, ok := c.attrs[key]; ok && v.StringValue != nil {
		return *v.StringValue
	}

	return ""
}

func (c sqsCarrier) Set(key, value string) {
	c.attrs[key] = types.MessageAttributeValue{
		DataType:    aws.String("String"),
		StringValue: aws.String(value),
	}
}

func (c sqsCarrier) Keys() []string {
	keys := make([]string, 0, len(c.attrs))

	for k := range c.attrs {
		keys = append(keys, k)
	}

	return keys
}

func InjectTraceContext(ctx context.Context, attrs map[string]types.MessageAttributeValue) {
	otel.GetTextMapPropagator().Inject(ctx, sqsCarrier{attrs: attrs})
}

func ExtractTraceContext(ctx context.Context, attrs map[string]types.MessageAttributeValue) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, sqsCarrier{attrs: attrs})
}
