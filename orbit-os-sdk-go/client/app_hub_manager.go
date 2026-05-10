package client

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	apphubv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/app_hub_service/v26"
	types "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/common"
)

// AppHubManager registers the app's WebUI with the Gravity portal (AppHubService).
// The portal derives the app's name and package_id from the caller's OS identity —
// the app only declares where its HTTP server listens and how to reach it.
type AppHubManager struct {
	client apphubv26.AppHubServiceClient
	ctx    context.Context
}

func NewAppHubManager(client apphubv26.AppHubServiceClient, ctx context.Context) *AppHubManager {
	return &AppHubManager{client: client, ctx: ctx}
}

// RegisterWebUI is the one-liner helper for the common case: an HTTP server
// listening on addr ("host:port" or ":port") with a TCP health-check.
// route is the path prefix the portal should proxy, e.g. "/myapp".
//
//	client.AppHubManager.RegisterWebUI("127.0.0.1:9033", "/myapp")
func (m *AppHubManager) RegisterWebUI(addr, route string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("AppHubManager.RegisterWebUI: invalid addr %q: %w", addr, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("AppHubManager.RegisterWebUI: invalid port in %q: %w", addr, err)
	}
	return m.RegisterService(&apphubv26.RegisterServiceRequest{
		Host:   host,
		Port:   int32(port),
		Routes: []*apphubv26.Route{{Path: route}},
		Health: &apphubv26.HealthCheck{
			Type: apphubv26.HealthCheckType_HEALTH_CHECK_TCP,
		},
	})
}

// RegisterService announces the app's HTTP server to the portal.
// The portal will proxy requests at the given route paths to host:port.
// name and package_id are derived server-side from the caller's UID — the app
// does not need to declare who it is.
// Prefer RegisterWebUI for the common HTTP + TCP-health-check case.
func (m *AppHubManager) RegisterService(req *apphubv26.RegisterServiceRequest) error {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	resp, err := m.client.RegisterService(ctx, req)
	if err != nil {
		return err
	}
	if e := resp.GetError(); e != nil && e.GetMessage() != "" {
		return &AppHubError{Code: e.GetCode().String(), Message: e.GetMessage()}
	}
	return nil
}

// UnregisterService removes the app's registration from the portal.
// Safe to call on shutdown.
func (m *AppHubManager) UnregisterService() error {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	_, err := m.client.UnregisterService(ctx, &types.Empty{})
	return err
}

// AddRoute adds an extra path prefix to an already-registered service.
func (m *AppHubManager) AddRoute(path string) error {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	_, err := m.client.AddRoute(ctx, &apphubv26.AddRouteRequest{
		Route: &apphubv26.Route{Path: path},
	})
	return err
}

// RemoveRoute removes a path prefix from the registered service.
func (m *AppHubManager) RemoveRoute(path string) error {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()
	_, err := m.client.RemoveRoute(ctx, &apphubv26.RemoveRouteRequest{Path: path})
	return err
}

// AppHubError carries a structured error from AppHubService.
type AppHubError struct {
	Code    string
	Message string
}

func (e *AppHubError) Error() string {
	return e.Code + ": " + e.Message
}
