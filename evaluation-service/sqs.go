package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Evento que será enviado para a fila
type EvaluationEvent struct {
	UserID      string    `json:"user_id"`
	FlagName    string    `json:"flag_name"`
	Result      bool      `json:"result"`
	Timestamp   time.Time `json:"timestamp"`
	Traceparent string    `json:"traceparent,omitempty"`
}

// sendEvaluationEvent envia um evento para a fila SQS propagando o traceparent W3C
func (a *App) sendEvaluationEvent(parentCtx context.Context, userID, flagName string, result bool) {
	spanCtx, span := otel.Tracer("evaluation-service").Start(
		parentCtx,
		"sqs.publish toggle-master-events",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "aws_sqs"),
			attribute.String("messaging.destination.name", "toggle-master-events"),
			attribute.String("peer.service", "toggle-master-events"),
			attribute.String("flag.name", flagName),
		),
	)
	defer span.End()

	// Se a URL da fila não foi configurada, apenas loga localmente e sai.
	if a.SqsSvc == nil || a.SqsQueueURL == "" {
		log.Printf("[SQS_DISABLED] Evento: User '%s', Flag '%s', Result '%t'", userID, flagName, result)
		return
	}

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(spanCtx, carrier)
	traceparent := carrier.Get("traceparent")

	event := EvaluationEvent{
		UserID:      userID,
		FlagName:    flagName,
		Result:      result,
		Timestamp:   time.Now().UTC(),
		Traceparent: traceparent,
	}

	body, err := json.Marshal(event)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Printf("Erro ao serializar evento SQS: %v", err)
		return
	}

	input := &sqs.SendMessageInput{
		MessageBody: aws.String(string(body)),
		QueueUrl:    aws.String(a.SqsQueueURL),
	}
	if traceparent != "" {
		input.MessageAttributes = map[string]*sqs.MessageAttributeValue{
			"traceparent": {
				DataType:    aws.String("String"),
				StringValue: aws.String(traceparent),
			},
		}
	}

	// Envia a mensagem
	_, err = a.SqsSvc.SendMessageWithContext(spanCtx, input)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Printf("Erro ao enviar mensagem para SQS: %v", err)
	} else {
		log.Printf("Evento de avaliação enviado para SQS (Flag: %s)", flagName)
	}
}
