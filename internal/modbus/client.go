// Package modbus implements a Modbus RTU client over an OrbitOS UART port.
// All reads are synchronous and serialised — call ReadRegisters from a single goroutine.
package modbus

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	sdkclient "github.com/OrbitOS-org/orbit-os-sdk-go/v26/client"
	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/logger"
)

const logTag = "modbus"
const uartSettleDelay = 500 * time.Millisecond
const preWriteSettleDelay = 100 * time.Millisecond  // Back to original
const postWriteSettleDelay = 50 * time.Millisecond  // Back to original
const requestTimeout = 2 * time.Second              // Back to original

// Client wraps the SDK UartManager to provide Modbus RTU request/response semantics.
type Client struct {
	uart              *sdkclient.UartManager
	port              string
	mu                sync.Mutex
	stateMu           sync.Mutex
	baseCtx           context.Context
	cfg               sdkclient.UartConfig
	open              bool
	consecutiveErrors int
}

func NewClient(uart *sdkclient.UartManager, port string) *Client {
	return &Client{uart: uart, port: port}
}

// Start opens the UART port and begins background byte accumulation.
func (c *Client) Start(ctx context.Context, cfg sdkclient.UartConfig) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.baseCtx = ctx
	c.cfg = cfg
	if err := c.openLocked("start"); err != nil {
		return err
	}
	// Extra delay after open to let inverter/port fully settle
	logger.Debugf(logTag, "Start: waiting %v for UART settle after open", uartSettleDelay)
	time.Sleep(uartSettleDelay)
	return nil
}

// Stop closes the UART stream and port. Any in-flight ReadRegisters call will
// return an error immediately once it notices the stopped flag.
func (c *Client) Stop() {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	_ = c.uart.Close(c.port)
	c.open = false
	time.Sleep(uartSettleDelay)
}

// ReadRegisters sends a Modbus request (fc=3 holding or fc=4 input) and returns
// the decoded register values. Timeout is 2 s; add 50 ms between consecutive calls.
func (c *Client) ReadRegisters(slaveID, fc byte, address, count uint16) (regs []uint16, err error) {
	c.mu.Lock()
	defer func() {
		// Watchdog: after repeated failures (any kind), force a full port reopen so
		// a permanent UART loss (device offline, gRPC stream stuck, etc.) is recovered
		// automatically without needing to restart the app.
		const watchdogThreshold = 5
		if err != nil {
			c.consecutiveErrors++
			if c.consecutiveErrors >= watchdogThreshold {
				logger.Warnf(logTag, "uart %s: %d consecutive errors, watchdog forcing reopen", c.port, c.consecutiveErrors)
				c.consecutiveErrors = 0
				if rerr := c.reopen(); rerr != nil {
					logger.Warnf(logTag, "uart watchdog reopen failed on %s: %v", c.port, rerr)
				}
			}
		} else {
			c.consecutiveErrors = 0
		}
		c.mu.Unlock()
	}()

	req := buildRequest(slaveID, fc, address, count)
	for attempt := 1; attempt <= 2; attempt++ {
		logger.Debugf(logTag, "writing modbus request (attempt %d): slaveID=%d fc=%d addr=%d count=%d hex=%s",
			attempt, slaveID, fc, address, count, toHex(req))
		var raw []byte
		raw, err = c.requestOnce(req, count)
		if err == nil {
			logger.Debugf(logTag, "parsing response: %s", toHex(raw))
			regs, err = parseResponse(raw, slaveID, fc, count)
			return
		}
		if attempt == 1 && isTransientReadError(err) {
			logger.Debugf(logTag, "modbus attempt %d transient failure on %s: %v", attempt, c.port, err)
		} else {
			logger.Warnf(logTag, "modbus attempt %d failed on %s: %v", attempt, c.port, err)
		}
		if attempt == 1 {
			// Reopen on port-level or stream failures. Plain read timeouts are treated
			// as transient individually, but the watchdog above handles persistent timeouts.
			errMsg := strings.ToLower(err.Error())
			shouldReopen := strings.Contains(errMsg, "writeuart") ||
				strings.Contains(errMsg, "openuart") ||
				strings.Contains(errMsg, "listenuart") ||
				strings.Contains(errMsg, "input/output error")
			if shouldReopen {
				if rerr := c.reopen(); rerr != nil {
					logger.Warnf(logTag, "uart reopen failed on %s: %v", c.port, rerr)
				}
			} else {
				logger.Debugf(logTag, "skip uart reopen on transient read error: %v", err)
			}
			time.Sleep(120 * time.Millisecond)
			continue
		}
		logger.Warnf(logTag, "modbus request failed after retries on %s: %v", c.port, err)
		return
	}
	err = fmt.Errorf("modbus request failed after retries")
	return
}

func (c *Client) openLocked(reason string) error {
	cfg := c.cfg
	cfg.Port = c.port
	logger.Debugf(logTag, "openLocked: configuring uart %s (%s): baud=%d, dataBits=%d, parity=%v, stopBits=%v, flowControl=%v",
		c.port, reason, cfg.Baudrate, cfg.DataBits, cfg.Parity, cfg.StopBits, cfg.FlowControl)
	
	// Best-effort cleanup in case a previous app instance left the port state
	// dangling on UartService. Ignore errors: port may already be closed.
	_ = c.uart.Close(c.port)
	time.Sleep(200 * time.Millisecond)
	var openErr error
	for i := 1; i <= 3; i++ {
		if err := c.uart.Open(cfg); err == nil {
			openErr = nil
			break
		} else {
			openErr = err
			logger.Warnf(logTag, "open uart %s failed (%s, attempt %d/3): %v", c.port, reason, i, err)
			time.Sleep(time.Duration(i) * 120 * time.Millisecond)
		}
	}
	if openErr != nil {
		return fmt.Errorf("open uart %s: %w", c.port, openErr)
	}
	c.open = true
	logger.Infof(logTag, "uart %s open ok (%s) - baud=%d, dataBits=%d, parity=%v, stopBits=%v",
		c.port, reason, cfg.Baudrate, cfg.DataBits, cfg.Parity, cfg.StopBits)
	return nil
}

func (c *Client) reopen() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	_ = c.uart.Close(c.port)
	c.open = false
	time.Sleep(uartSettleDelay)
	return c.openLocked("reopen")
}

func (c *Client) requestOnce(req []byte, count uint16) ([]byte, error) {
	parent := c.baseCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()

	logger.Debugf(logTag, "requestOnce: starting listen on %s (expecting ~%d bytes)", c.port, 5+int(count)*2)

	// ListenAsync establishes the gRPC stream synchronously (blocks until the
	// server sends headers back). This guarantees the UartService has finished
	// its internal port/RS485 setup before WriteUart arrives, which was the root
	// cause of the EIO: the previous goroutine+sleep(40ms) approach let WriteUart
	// race ahead of the still-initialising ListenUart handler.
	time.Sleep(preWriteSettleDelay)
	
	ch, err := c.uart.ListenAsync(ctx, c.port, 512)
	if err != nil {
		return nil, fmt.Errorf("listen uart: %w", err)
	}
	logger.Debugf(logTag, "requestOnce: listen established, writing request: %s", toHex(req))

	time.Sleep(postWriteSettleDelay)

	if _, err := c.uart.Write(c.port, req); err != nil {
		return nil, fmt.Errorf("write modbus request: %w", err)
	}
	logger.Debugf(logTag, "requestOnce: write complete, waiting for response")

	var (
		raw  []byte
		want = 5 + int(count)*2
	)

	for data := range ch {
		logger.Debugf(logTag, "requestOnce: received %d bytes: %s", len(data), toHex(data))
		raw = append(raw, data...)
		beforeTrim := len(raw)
		raw = trimToSlaveFrame(raw, req[0])
		afterTrim := len(raw)
		if beforeTrim != afterTrim {
			logger.Debugf(logTag, "requestOnce: trimmed %d bytes (noise), buffer now %d bytes", beforeTrim-afterTrim, afterTrim)
		}
		
		if len(raw) >= 3 {
			if raw[1]&0x80 != 0 {
				want = 5
				logger.Debugf(logTag, "requestOnce: detected exception response, expecting 5 bytes total")
			} else {
				byteCount := int(raw[2])
				want = 5 + byteCount
				logger.Debugf(logTag, "requestOnce: byte count in response: %d, expecting %d bytes total", byteCount, want)
			}
		}
		
		if len(raw) >= want {
			logger.Debugf(logTag, "requestOnce: got enough bytes (%d >= %d), cancelling context", len(raw), want)
			cancel()
		}
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("no response received (timeout)")
	}
	if len(raw) < want {
		return nil, fmt.Errorf("partial modbus response: got %d bytes, want %d (raw=%s)", len(raw), want, toHex(raw))
	}
	logger.Debugf(logTag, "requestOnce: complete response: %s", toHex(raw))
	return raw, nil
}

func buildRequest(slaveID, fc byte, address, count uint16) []byte {
	pdu := make([]byte, 6)
	pdu[0] = slaveID
	pdu[1] = fc
	binary.BigEndian.PutUint16(pdu[2:], address)
	binary.BigEndian.PutUint16(pdu[4:], count)
	crc := crc16(pdu)
	return append(pdu, byte(crc), byte(crc>>8)) // CRC is little-endian on the wire
}

func parseResponse(buf []byte, slaveID, fc byte, count uint16) ([]uint16, error) {
	want := 5 + int(count)*2
	logger.Debugf(logTag, "parseResponse: slaveID=0x%02x fc=0x%02x count=%d, want=%d bytes, got=%d bytes", slaveID, fc, count, want, len(buf))
	
	if len(buf) < want {
		return nil, fmt.Errorf("short response: got %d bytes, want %d", len(buf), want)
	}
	
	logger.Debugf(logTag, "parseResponse: checking slave id: buf[0]=0x%02x vs expected=0x%02x", buf[0], slaveID)
	if buf[0] != slaveID {
		return nil, fmt.Errorf("slave id mismatch: got %d, want %d", buf[0], slaveID)
	}
	
	logger.Debugf(logTag, "parseResponse: checking fc: buf[1]=0x%02x vs expected=0x%02x", buf[1], fc)
	if buf[1]&0x80 != 0 { // exception response
		exceptionCode := buf[2]
		logger.Errorf(logTag, "parseResponse: EXCEPTION response received: code=%d (0x%02x)", exceptionCode, exceptionCode)
		return nil, fmt.Errorf("modbus exception %d (fc 0x%02x)", buf[2], buf[1]&0x7F)
	}
	
	if buf[1] != fc {
		return nil, fmt.Errorf("fc mismatch: got 0x%02x, want 0x%02x", buf[1], fc)
	}
	
	logger.Debugf(logTag, "parseResponse: checking CRC")
	calc := crc16(buf[:want-2])
	got := uint16(buf[want-2]) | uint16(buf[want-1])<<8
	logger.Debugf(logTag, "parseResponse: CRC - calculated=0x%04x received=0x%04x", calc, got)
	if calc != got {
		return nil, fmt.Errorf("crc mismatch: calc=0x%04x got=0x%04x", calc, got)
	}
	
	regs := make([]uint16, count)
	for i := range regs {
		regs[i] = binary.BigEndian.Uint16(buf[3+2*i:])
		logger.Debugf(logTag, "parseResponse: reg[%d] = 0x%04x (%d)", i, regs[i], regs[i])
	}
	logger.Debugf(logTag, "parseResponse: success, parsed %d registers", count)
	return regs, nil
}

func toHex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	enc := make([]byte, hex.EncodedLen(len(b)))
	hex.Encode(enc, b)
	out := make([]byte, 0, len(enc)+len(enc)/2)
	for i := 0; i < len(enc); i += 2 {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, enc[i], enc[i+1])
	}
	return string(out)
}

func trimToSlaveFrame(raw []byte, slaveID byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	// Skip leading noise bytes until the expected slave id.
	for i := 0; i < len(raw); i++ {
		if raw[i] == slaveID {
			if i > 0 {
				logger.Debugf(logTag, "trimToSlaveFrame: discarded %d noise bytes before slave ID 0x%02x: %s", i, slaveID, toHex(raw[:i]))
			}
			return raw[i:]
		}
	}
	// No slave-id found; treat as noise.
	logger.Warnf(logTag, "trimToSlaveFrame: no slave ID 0x%02x found in buffer, discarding all %d bytes: %s", slaveID, len(raw), toHex(raw))
	return nil
}

func isTransientReadError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no response received (timeout)") ||
		strings.Contains(msg, "partial modbus response")
}
