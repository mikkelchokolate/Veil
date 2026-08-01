package privileged

import "context"

type restartPanelContextKey struct{}

func ContextWithRestartPanelRequest(ctx context.Context, request RestartPanelRequest) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, restartPanelContextKey{}, request)
}

func RestartPanelRequestFromContext(ctx context.Context) (RestartPanelRequest, bool) {
	if ctx == nil {
		return RestartPanelRequest{}, false
	}
	request, ok := ctx.Value(restartPanelContextKey{}).(RestartPanelRequest)
	return request, ok
}
