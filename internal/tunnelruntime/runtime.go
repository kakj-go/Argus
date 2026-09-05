// Package tunnelruntime contains the transport primitives shared by the
// Direct Executor and the on-host Connector tunnel supervisors.
package tunnelruntime

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/time/rate"
)

const (
	LeaseDuration     = 90 * time.Second
	HeartbeatInterval = 30 * time.Second
	ForwardBufferSize = 32 * 1024
)

var retryBackoff = [...]time.Duration{
	time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	30 * time.Second,
}

// Backoff returns the fixed PlanV4 reconnect sequence, capped at 30 seconds.
func Backoff(attempt int) time.Duration {
	if attempt <= 1 {
		return retryBackoff[0]
	}
	if attempt >= len(retryBackoff) {
		return retryBackoff[len(retryBackoff)-1]
	}
	return retryBackoff[attempt-1]
}

// Counters keeps cumulative bytes and a delta since the previous database
// heartbeat. The delta operation is safe while relays are active.
type Counters struct {
	bytes          atomic.Int64
	reportedBytes  atomic.Int64
	throttled      atomic.Int64
	reportedLimits atomic.Int64
}

func (c *Counters) AddBytes(value int64) {
	if value > 0 {
		c.bytes.Add(value)
	}
}

func (c *Counters) AddThrottled() { c.throttled.Add(1) }

func (c *Counters) TotalBytes() int64 { return c.bytes.Load() }

func (c *Counters) Delta() (bytes, throttled int64) {
	totalBytes := c.bytes.Load()
	previousBytes := c.reportedBytes.Swap(totalBytes)
	totalLimits := c.throttled.Load()
	previousLimits := c.reportedLimits.Swap(totalLimits)
	return max(0, totalBytes-previousBytes), max(0, totalLimits-previousLimits)
}

// Relay bridges accepted remote-forward connections to one local target. A
// single limiter can be shared by all Relay values in one process so the
// configured bytes-per-second quota is aggregate rather than per connection.
type Relay struct {
	Target   string
	Kind     string
	Limiter  *rate.Limiter
	Counters *Counters
	Dialer   net.Dialer
}

func NewLimiter(bytesPerSecond int64) *rate.Limiter {
	if bytesPerSecond <= 0 {
		return nil
	}
	burst := int(min(bytesPerSecond, int64(4*ForwardBufferSize)))
	if burst < ForwardBufferSize {
		burst = ForwardBufferSize
	}
	return rate.NewLimiter(rate.Limit(bytesPerSecond), burst)
}

func (relay Relay) Serve(ctx context.Context, listener net.Listener) error {
	if relay.Target == "" || listener == nil {
		return errors.New("tunnel relay target and listener are required")
	}
	for {
		inbound, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go relay.bridge(ctx, inbound)
	}
}

func (relay Relay) bridge(ctx context.Context, inbound net.Conn) {
	defer inbound.Close()
	upstream, err := relay.Dialer.DialContext(ctx, "tcp", relay.Target)
	if err != nil {
		return
	}
	defer upstream.Close()
	done := make(chan struct{}, 2)
	copyDirection := func(destination net.Conn, source net.Conn) {
		writer := &meteredWriter{ctx: ctx, target: destination, limiter: relay.Limiter,
			counters: relay.Counters, kind: relay.Kind}
		_, _ = io.CopyBuffer(writer, source, make([]byte, ForwardBufferSize))
		done <- struct{}{}
	}
	go copyDirection(upstream, inbound)
	go copyDirection(inbound, upstream)
	select {
	case <-ctx.Done():
	case <-done:
	}
	_ = inbound.SetDeadline(time.Now())
	_ = upstream.SetDeadline(time.Now())
}

type meteredWriter struct {
	ctx      context.Context
	target   io.Writer
	limiter  *rate.Limiter
	counters *Counters
	kind     string
}

func (writer *meteredWriter) Write(value []byte) (int, error) {
	written := 0
	for len(value) > 0 {
		chunkSize := len(value)
		if writer.limiter != nil && chunkSize > writer.limiter.Burst() {
			chunkSize = writer.limiter.Burst()
		}
		chunk := value[:chunkSize]
		if writer.limiter != nil && !writer.limiter.AllowN(time.Now(), chunkSize) {
			if writer.counters != nil {
				writer.counters.AddThrottled()
			}
			RecordThrottled(writer.kind)
			if err := writer.limiter.WaitN(writer.ctx, chunkSize); err != nil {
				return written, err
			}
		}
		count, err := writer.target.Write(chunk)
		written += count
		if writer.counters != nil {
			writer.counters.AddBytes(int64(count))
		}
		RecordBytes(writer.kind, count)
		if err != nil {
			return written, err
		}
		if count != chunkSize {
			return written, io.ErrShortWrite
		}
		value = value[chunkSize:]
	}
	return written, nil
}

// Keepalive blocks until cancellation or an SSH keepalive failure.
func Keepalive(ctx context.Context, client *ssh.Client, interval time.Duration) error {
	if client == nil {
		return errors.New("SSH client is required")
	}
	if interval <= 0 {
		interval = HeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				return err
			}
		}
	}
}
