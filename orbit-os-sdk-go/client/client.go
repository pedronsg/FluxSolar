package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	aiv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/ai_service/v26"
	apphubv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/app_hub_service/v26"
	authv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/auth_service/v26"
	btsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/bluetooth_service/v26"
	camerav26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/camera_service/v26"
	devsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/development_service/v26"
	ethsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/ethernet_service/v26"
	eventsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/event_service/v26"
	fwsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/firewall_service/v26"
	gpiosvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/gpio_service/v26"
	i2csvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/i2c_service/v26"
	pmv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/package_service/v26"
	powerv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/power_service/v26"
	pwmsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/pwm_service/v26"
	spiscvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/spi_service/v26"
	systemv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/system_service/v26"
	uartsvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/uart_service/v26"
	updatesvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/update_service/v26"
	vpnv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/vpn_service/v26"
	wifisvcv26 "github.com/OrbitOS-org/orbit-os-sdk-go/v26/api/wifi_service/v26"
	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/logger"
)

const unixSocket = "/run/gravity/ipc/system_server.sock"
const tcpPort = 6000

const logTag = "client"

// grpcMaxMsgSize is the default cap for both send and receive gRPC messages.
// The default (4 MiB) is too small for AI tensors (e.g. 640×640×3 float32 ≈ 4.7 MiB).
const grpcMaxMsgSize = 64 * 1024 * 1024 // 64 MiB

// TLSConfig holds paths to TLS certificate material for the gRPC client.
type TLSConfig struct {
	// CAFile is the CA certificate used to verify the server (required for TLS).
	CAFile string

	// CertFile and KeyFile are optional; set both for mutual TLS (client certificate).
	CertFile string // Client certificate
	KeyFile  string // Client private key

	// ServerName is the expected server CN or a SAN (DNS/IP). If empty, only the CA
	// chain is verified — any server cert signed by the CA is accepted (use ORBIT_GRPC_TLS_SERVER_NAME to pin).
	ServerName string
}

type Client struct {
	conn               *grpc.ClientConn
	ctx                context.Context
	cancel             context.CancelFunc
	AIManager          *AIManager
	AppHubManager      *AppHubManager
	AuthManager        *AuthManager
	PowerManager       *PowerManager
	SystemManager      *SystemManager
	EthernetManager    *EthernetManager
	WiFiManager        *WiFiManager
	DevelopmentManager *DevelopmentManager
	FirewallManager    *FirewallManager
	GpioManager        *GpioManager
	PwmManager         *PwmManager
	UartManager        *UartManager
	I2CManager         *I2CManager
	SpiManager         *SpiManager
	BluetoothManager   *BluetoothManager
	PackageManager     *PackageManager
	CameraManager      *CameraManager
	VPNManager         *VPNManager
	EventManager       *EventManager
	UpdateManager      *UpdateManager
}

// Recommended layout in the SDK repo:
//
//	orbitos-sdk-go/certs/grpc/ca.crt
//	orbitos-sdk-go/certs/grpc/client.crt   (optional, mTLS)
//	orbitos-sdk-go/certs/grpc/client.key
//
// Search walks up from the working directory (finds repo root even when running
// `go run ./cmd/examples/...` from a subdirectory).
func loadTLSConfigFromDefaultPaths() (*TLSConfig, error) {
	const sub = "certs/grpc"
	caName := "ca.crt"
	clientCrtName := "client.crt"
	clientKeyName := "client.key"

	var dirs []string
	seen := make(map[string]bool)
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	if cwd, err := os.Getwd(); err == nil {
		for d := cwd; d != ""; {
			add(filepath.Join(d, sub))
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}

	if exe, err := os.Executable(); err == nil {
		exDir := filepath.Dir(exe)
		if strings.Contains(exDir, "/tmp/go-build") || strings.Contains(exDir, "/.cache/go-build") || strings.Contains(exDir, "go-build") {
			if cwd, err := os.Getwd(); err == nil {
				exDir = cwd
			}
		}
		add(filepath.Join(exDir, sub))
	}

	for _, d := range dirs {
		ca := filepath.Join(d, caName)
		if _, err := os.Stat(ca); err == nil {
			cfg := &TLSConfig{
				CAFile:   ca,
				CertFile: filepath.Join(d, clientCrtName),
				KeyFile:  filepath.Join(d, clientKeyName),
			}
			if sn := strings.TrimSpace(os.Getenv("ORBIT_GRPC_TLS_SERVER_NAME")); sn != "" {
				cfg.ServerName = sn
			}
			return cfg, nil
		}
	}

	return nil, fmt.Errorf("TLS certs: put %s/ca.crt under orbitos-sdk-go/certs/grpc/ (or another cwd ancestor) or next to the executable", sub)
}

// NewTCPClient creates a gRPC client over TCP with TLS.
func NewTCPClient(host string, port int) (*Client, error) {
	tlsConfig, err := loadTLSConfigFromDefaultPaths()
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS config: %w", err)
	}

	address := fmt.Sprintf("%s:%d", host, port)
	return newClientWithTCP(address, tlsConfig)
}

// GetSDKAPIVersion returns the gRPC API revision this SDK was built against
func GetSDKAPIVersion() (version int32) {
	return int32(systemv26.APIVersionInfo_VERSION)
}

// GetSDKAPIRevision returns the gRPC API revision this SDK was built against
func GetSDKAPIRevision() (revision int32) {
	return int32(systemv26.APIRevisionInfo_REVISION)
}

// GetSDKAPIVersionInfo returns the gRPC API version and revision this SDK was built against
func GetSDKAPIVersionInfo() string {
	return fmt.Sprintf("%d.%d", systemv26.APIVersionInfo_VERSION, systemv26.APIRevisionInfo_REVISION)
}

// NewUDSClient creates a gRPC client over a Unix domain socket.
// Note: UDS does not use TLS (local socket).
func NewUDSClient() (*Client, error) {
	return newClientWithUDS(unixSocket)
}

func newClientWithTCP(address string, tlsConfig *TLSConfig) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background())

	var creds credentials.TransportCredentials
	var err error

	if tlsConfig != nil && tlsConfig.CAFile != "" {
		creds, err = loadTLSClientCredentials(tlsConfig)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
		}
	} else {
		// No TLS (insecure)
		creds = insecure.NewCredentials()
	}

	ctxDial, cancelDial := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelDial()

	conn, err := grpc.DialContext(ctxDial, address,
		grpc.WithTransportCredentials(creds),
		grpc.WithBlock(),
		grpc.WithReturnConnectionError(), // surface dial errors immediately
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(grpcMaxMsgSize),
			grpc.MaxCallRecvMsgSize(grpcMaxMsgSize),
		),
	)
	if err != nil {
		cancel()
		if tlsConfig != nil && tlsConfig.CAFile != "" {
			return nil, fmt.Errorf("failed to connect to %s with TLS: %w (check if server is running with TLS enabled and certificates are valid)", address, err)
		}
		return nil, fmt.Errorf("failed to connect to %s: %w (check if server is running)", address, err)
	}

	return createClientFromConnection(conn, ctx, cancel), nil
}

// compareSDKAndDeviceAPIVersions compares the SDK API Version with the Device API Version
func compareSDKAndDeviceAPIVersions(client *Client) error {

	// SDK API Version
	sdkVersion := GetSDKAPIVersionInfo()

	// Client API Version
	clientVersion, err := client.SystemManager.GetApiVersionInfo()
	if err != nil {
		logger.Errorf(logTag, "GetApiVersionInfo: %v", err)
		return err
	}

	logger.Infof(logTag, "SDK API Version: %s", sdkVersion)
	logger.Infof(logTag, "Dev API Version: %s", clientVersion)

	// If the API Versions do not match, return a warning
	if clientVersion != sdkVersion {
		logger.Warnf(logTag, "SDK API Version: %s does not match Device API Version: %s", sdkVersion, clientVersion)
	}
	return nil
}

func newClientWithUDS(socketPath string) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background())

	dialAddress := "unix://" + socketPath
	creds := insecure.NewCredentials()

	ctxDial, cancelDial := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelDial()

	conn, err := grpc.DialContext(ctxDial, dialAddress,
		grpc.WithTransportCredentials(creds),
		grpc.WithBlock(),
		grpc.WithReturnConnectionError(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(grpcMaxMsgSize),
			grpc.MaxCallRecvMsgSize(grpcMaxMsgSize),
		),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to Unix socket %s: %w (check if server is running and socket exists)", socketPath, err)
	}

	client := createClientFromConnection(conn, ctx, cancel)

	return client, nil
}

// attach calls SystemService.Attach() to signal the Gravity watchdog.
// func (c *Client) attach() error {
// 	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
// 	defer cancel()

// 	resp, err := c.SystemManager.client.Attach(ctx, &commonv26.Empty{})
// 	if err != nil {
// 		return err
// 	}
// 	if !resp.GetValue() {
// 		return fmt.Errorf("Attach returned false (UID not registered as an installed app)")
// 	}
// 	return nil
// }

// createClientFromConnection builds a Client from an established gRPC connection and wires all managers.
func createClientFromConnection(conn *grpc.ClientConn, ctx context.Context, cancel context.CancelFunc) *Client {
	c := &Client{
		conn:   conn,
		ctx:    ctx,
		cancel: cancel,
	}

	c.AIManager = NewAIManager(aiv26.NewAiServiceClient(conn), ctx)
	c.AppHubManager = NewAppHubManager(apphubv26.NewAppHubServiceClient(conn), ctx)
	c.AuthManager = NewAuthManager(authv26.NewAuthServiceClient(conn), ctx)
	c.PowerManager = NewPowerManager(powerv26.NewPowerServiceClient(conn), ctx)
	c.SystemManager = NewSystemManager(systemv26.NewSystemServiceClient(conn), ctx)
	c.EthernetManager = NewEthernetManager(ethsvcv26.NewEthernetServiceClient(conn), ctx)
	c.WiFiManager = NewWiFiManager(wifisvcv26.NewWiFiServiceClient(conn), ctx)
	c.DevelopmentManager = NewDevelopmentManager(devsvcv26.NewDevelopmentServiceClient(conn), ctx)
	c.FirewallManager = NewFirewallManager(fwsvcv26.NewFirewallServiceClient(conn), ctx)
	c.GpioManager = NewGpioManager(gpiosvcv26.NewGpioServiceClient(conn), ctx)
	c.PwmManager = NewPwmManager(pwmsvcv26.NewPwmServiceClient(conn), ctx)
	c.UartManager = NewUartManager(uartsvcv26.NewUartServiceClient(conn), ctx)
	c.I2CManager = NewI2CManager(i2csvcv26.NewI2CServiceClient(conn), ctx)
	c.SpiManager = NewSpiManager(spiscvcv26.NewSpiServiceClient(conn), ctx)
	c.BluetoothManager = newBluetoothManager(btsvcv26.NewBluetoothServiceClient(conn))
	c.PackageManager = NewPackageManager(pmv26.NewPackageManagerServiceClient(conn), ctx)
	c.CameraManager = NewCameraManager(camerav26.NewCameraServiceClient(conn))
	c.VPNManager = NewVPNManager(vpnv26.NewVPNServiceClient(conn), ctx)
	c.EventManager = NewEventManager(eventsvcv26.NewEventServiceClient(conn), ctx)
	c.UpdateManager = NewUpdateManager(updatesvcv26.NewUpdateServiceClient(conn), ctx)

	return c
}

// loadTLSClientCredentials loads TLS credentials for the client.
// The CA is required to verify the server and prevent MITM; without it a fake server cert could be accepted.
func loadTLSClientCredentials(config *TLSConfig) (credentials.TransportCredentials, error) {
	if config.CAFile == "" {
		return nil, fmt.Errorf("CAFile is required for TLS - without CA, client cannot verify server identity (MITM vulnerability)")
	}

	caCert, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	serverName := strings.TrimSpace(config.ServerName)

	// Custom TLS verification to support legacy certs (CN only, no SANs).
	// Go 1.15+ rejects certs without SANs by default, so we disable default verify
	// and implement VerifyPeerCertificate manually.
	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName, // SNI; empty is OK when dialing an IP
		InsecureSkipVerify: true,       // default verify disabled; we verify below
	}

	tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("no certificates provided")
		}

		cert, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("failed to parse certificate: %w", err)
		}

		opts := x509.VerifyOptions{
			Roots: caCertPool,
		}

		_, err = cert.Verify(opts)
		if err != nil {
			return fmt.Errorf("certificate verification failed: %w", err)
		}

		// No hostname pin: trust ends at CA signature (typical for dev / mTLS with arbitrary server CN).
		if serverName == "" {
			return nil
		}

		if len(cert.DNSNames) == 0 && len(cert.IPAddresses) == 0 {
			if cert.Subject.CommonName != serverName {
				return fmt.Errorf("certificate CN (%s) does not match ServerName (%s)", cert.Subject.CommonName, serverName)
			}
		} else {
			found := false
			for _, dnsName := range cert.DNSNames {
				if dnsName == serverName {
					found = true
					break
				}
			}
			if !found {
				for _, ip := range cert.IPAddresses {
					if ip.String() == serverName {
						found = true
						break
					}
				}
				if !found && cert.Subject.CommonName != serverName {
					return fmt.Errorf("certificate does not match ServerName (%s)", serverName)
				}
			}
		}

		return nil
	}

	if config.CertFile != "" && config.KeyFile != "" {
		clientCert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{clientCert}
	}

	return credentials.NewTLS(tlsConfig), nil
}

// SocketExists returns true if the Unix socket path exists.
func socketExists(socketPath string) bool {
	if _, err := os.Stat(socketPath); err == nil {
		return true
	}
	return false
}

// NewClientAuto picks UDS if the Unix socket exists, otherwise TCP+TLS.
// After connecting, it always compares the SDK API version with the device (see compareSDKAndDeviceAPIVersions).
func NewClientAuto(tcpHost string) (*Client, error) {
	var client *Client
	var err error
	if socketExists(unixSocket) {
		client, err = NewUDSClient()
	} else {
		client, err = NewTCPClient(tcpHost, tcpPort)
	}
	if err != nil {
		return nil, err
	}
	if err := compareSDKAndDeviceAPIVersions(client); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) Close() error {
	c.cancel()
	return c.conn.Close()
}
