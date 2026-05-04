package tunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	trycloudflareURLRe = regexp.MustCompile(`https?://[a-z0-9-]+\.trycloudflare\.com`)
	connRegisteredRe   = regexp.MustCompile(`Registered tunnel connection`)
)

// StartCloudflared runs `cloudflared tunnel --url http://localhost:<port>` as
// a child process and returns the public *.trycloudflare.com URL it announces
// in its log output, plus a stop function that terminates the tunnel. The
// returned URL is host-only (no path), e.g.
// "https://wide-pebble-tasty-mango.trycloudflare.com".
//
// cloudflared must be on PATH. The function blocks for up to 30 seconds
// waiting for the URL to appear; if the binary exits or the timeout elapses
// first, it returns an error.
func StartCloudflared(ctx context.Context, port int) (string, func(), error) {
	if _, err := exec.LookPath("cloudflared"); err != nil {
		return "", nil, fmt.Errorf("cloudflared not found on PATH: %w", err)
	}

	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, "cloudflared",
		"tunnel",
		"--no-autoupdate",
		"--url", fmt.Sprintf("http://localhost:%d", port),
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", nil, fmt.Errorf("cloudflared stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", nil, fmt.Errorf("cloudflared stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, fmt.Errorf("start cloudflared: %w", err)
	}

	urlCh := make(chan string, 1)
	var once sync.Once
	scan := func(r io.Reader) {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for s.Scan() {
			if m := trycloudflareURLRe.FindString(s.Text()); m != "" {
				once.Do(func() { urlCh <- m })
			}
		}
	}
	go scan(stdout)
	go scan(stderr)

	waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer waitCancel()

	select {
	case url := <-urlCh:
		stop := func() {
			cancel()
			_ = cmd.Wait()
		}
		return url, stop, nil
	case <-waitCtx.Done():
		cancel()
		_ = cmd.Wait()
		return "", nil, errors.New("cloudflared did not announce a public URL within 30s")
	}
}

// StartNamedTunnel runs `cloudflared tunnel run --url http://localhost:<port>
// <name>`, which connects a pre-created named tunnel to the local server.
//
// Prerequisite (one-time, run by the operator before launching the app):
//
//	cloudflared tunnel login
//	cloudflared tunnel create <name>
//	cloudflared tunnel route dns <name> <hostname>
//
// hostname is the DNS name you routed to the tunnel (e.g. "stream.example.com").
// It is returned as the public URL — cloudflared itself does not print one,
// so we mirror back what the operator already configured.
//
// The function blocks for up to 30 seconds waiting for the first
// "Registered tunnel connection" line in cloudflared's logs. If the binary
// exits or the timeout elapses first, it returns an error.
func StartNamedTunnel(ctx context.Context, port int, name, hostname string) (string, func(), error) {
	if name == "" {
		return "", nil, errors.New("named tunnel: name is empty")
	}
	if hostname == "" {
		return "", nil, errors.New("named tunnel: hostname is empty")
	}
	if _, err := exec.LookPath("cloudflared"); err != nil {
		return "", nil, fmt.Errorf("cloudflared not found on PATH: %w", err)
	}

	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, "cloudflared",
		"tunnel",
		"--no-autoupdate",
		"run",
		"--url", fmt.Sprintf("http://localhost:%d", port),
		name,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", nil, fmt.Errorf("cloudflared stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", nil, fmt.Errorf("cloudflared stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, fmt.Errorf("start cloudflared: %w", err)
	}

	readyCh := make(chan struct{}, 1)
	var once sync.Once
	scan := func(r io.Reader) {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for s.Scan() {
			if connRegisteredRe.MatchString(s.Text()) {
				once.Do(func() { readyCh <- struct{}{} })
			}
		}
	}
	go scan(stdout)
	go scan(stderr)

	waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer waitCancel()

	publicURL := hostname
	if !strings.Contains(publicURL, "://") {
		publicURL = "https://" + publicURL
	}

	select {
	case <-readyCh:
		stop := func() {
			cancel()
			_ = cmd.Wait()
		}
		return publicURL, stop, nil
	case <-waitCtx.Done():
		cancel()
		_ = cmd.Wait()
		return "", nil, errors.New("cloudflared named tunnel did not register a connection within 30s")
	}
}
