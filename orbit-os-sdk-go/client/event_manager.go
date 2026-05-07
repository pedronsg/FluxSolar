package client

import (
	"context"
	"io"

	eventsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/event_service/v26"
)

type EventManager struct {
	client eventsvcv26.EventServiceClient
	ctx    context.Context
}

func NewEventManager(client eventsvcv26.EventServiceClient, ctx context.Context) *EventManager {
	return &EventManager{client: client, ctx: ctx}
}

// Subscribe opens a server-side stream and calls handler for each received event.
// Pass no types to receive all events.
// Blocks until the context is cancelled, the server closes the stream, or an error occurs.
func (e *EventManager) Subscribe(ctx context.Context, handler func(*eventsvcv26.Event), types ...eventsvcv26.EventType) error {
	req := &eventsvcv26.SubscribeRequest{Types: types}
	stream, err := e.client.Subscribe(ctx, req)
	if err != nil {
		return err
	}
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		handler(ev)
	}
}
