package admin

import (
	"context"
	"time"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
)

func defaultAdminBatchNSFWSequence(ctx context.Context, token string) error {
	return adminBatchAuthClient().NSFWSequence(ctx, token)
}

func defaultAdminBatchSetNSFW(ctx context.Context, token string, enabled bool) error {
	_, err := adminBatchAuthClient().SetNSFW(ctx, token, enabled)
	return err
}

func adminBatchAuthClient() *protocol.XAIAuthClient {
	return protocol.NewXAIAuthClient(protocol.AuthClientOptions{
		GRPC: transport.GRPCWebTransport{},
		JSON: adminBatchJSONAuthTransport{},
	})
}

type adminBatchJSONAuthTransport struct{}

func (adminBatchJSONAuthTransport) PostJSON(ctx context.Context, request protocol.JSONAuthRequest) (map[string]any, error) {
	timeout := time.Duration(request.TimeoutS * float64(time.Second))
	return transport.PostJSON(ctx, request.URL, request.Token, request.Payload, transport.HTTPOptions{
		Lease:       request.Lease,
		Timeout:     timeout,
		ContentType: "application/json",
		Origin:      request.Origin,
		Referer:     request.Referer,
	})
}
