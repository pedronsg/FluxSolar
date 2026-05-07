package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"sync"
	"time"

	authv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/auth_service/v26"
	"google.golang.org/grpc"
)

const sdkCertCN = "Orbit OS SDK Development Unit"

// mutableTokenCreds implements credentials.PerRPCCredentials with a token that
// can be set after the connection is established. Before the token is set
// (e.g. during the Login call itself) no header is injected.
type mutableTokenCreds struct {
	mu    sync.RWMutex
	token string
}

func (t *mutableTokenCreds) set(token string) {
	t.mu.Lock()
	t.token = token
	t.mu.Unlock()
}

func (t *mutableTokenCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.token == "" {
		return map[string]string{}, nil
	}
	return map[string]string{"authorization": "Bearer " + t.token}, nil
}

func (t *mutableTokenCreds) RequireTransportSecurity() bool {
	return true
}

// certCN reads the CN from the certificate at certFile/keyFile.
func certCN(certFile, keyFile string) (string, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return "", fmt.Errorf("load cert: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return "", fmt.Errorf("no certificates found")
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "", fmt.Errorf("parse cert: %w", err)
	}
	return cert.Subject.CommonName, nil
}

// NewToolClient connects to the Gravity runtime over TCP+mTLS.
//
// If the loaded certificate is the SDK cert, it connects without token auth
// (a warning is logged — dev mode must be enabled on the device).
//
// For any other certificate (tool cert) it performs Login automatically and
// injects the session token into every subsequent call via PerRPCCredentials.
func NewToolClient(host string, username, password string) (*Client, error) {
	tlsConfig, err := loadTLSConfigFromDefaultPaths()
	if err != nil {
		return nil, fmt.Errorf("tool client: load TLS config: %w", err)
	}

	cn, err := certCN(tlsConfig.CertFile, tlsConfig.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("tool client: read cert CN: %w", err)
	}

	isSDK := cn == sdkCertCN
	if isSDK {
		log.Printf("[WARN] tool client: SDK certificate detected (CN=%q) — connecting without token auth; developer mode must be enabled on the device", cn)
		return newClientWithTCP(fmt.Sprintf("%s:%d", host, tcpPort), tlsConfig)
	}

	// Tool cert — single connection with mutable token creds.
	tokenCreds := &mutableTokenCreds{}

	creds, err := loadTLSClientCredentials(tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("tool client: load TLS credentials: %w", err)
	}

	address := fmt.Sprintf("%s:%d", host, tcpPort)
	ctx, cancel := context.WithCancel(context.Background())

	ctxDial, cancelDial := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelDial()

	conn, err := grpc.DialContext(ctxDial, address,
		grpc.WithTransportCredentials(creds),
		grpc.WithPerRPCCredentials(tokenCreds),
		grpc.WithBlock(),
		grpc.WithReturnConnectionError(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(grpcMaxMsgSize),
			grpc.MaxCallRecvMsgSize(grpcMaxMsgSize),
		),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("tool client: connect: %w", err)
	}

	// Login — token is empty so the server exempts this call, no header sent.
	authClient := authv26.NewAuthServiceClient(conn)
	ctxLogin, cancelLogin := context.WithTimeout(ctx, 10*time.Second)
	defer cancelLogin()

	resp, err := authClient.Login(ctxLogin, &authv26.LoginRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		cancel()
		conn.Close()
		return nil, fmt.Errorf("tool client: login failed: %w", err)
	}

	// Activate token — all subsequent calls will include it automatically.
	tokenCreds.set(resp.GetToken())

	return createClientFromConnection(conn, ctx, cancel), nil
}
