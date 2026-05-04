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
// The function blocks until BOTH (a) the public URL has been announced and
// (b) cloudflared has registered at least one connection to the Cloudflare
// edge — until both happen, hitting the URL returns Cloudflare error 1033.
// cloudflared must be on PATH. Times out after 60s.
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
	connCh := make(chan struct{}, 1)
	var urlOnce, connOnce sync.Once
	scan := func(r io.Reader) {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for s.Scan() {
			line := s.Text()
			if m := trycloudflareURLRe.FindString(line); m != "" {
				urlOnce.Do(func() { urlCh <- m })
			}
			if connRegisteredRe.MatchString(line) {
				connOnce.Do(func() { connCh <- struct{}{} })
			}
		}
	}
	go scan(stdout)
	go scan(stderr)

	waitCtx, waitCancel := context.WithTimeout(ctx, 60*time.Second)
	defer waitCancel()

	var url string
	gotURL := false
	gotConn := false
	for !gotURL || !gotConn {
		select {
		case u := <-urlCh:
			url = u
			gotURL = true
		case <-connCh:
			gotConn = true
		case <-waitCtx.Done():
			cancel()
			_ = cmd.Wait()
			switch {
			case !gotURL:
				return "", nil, errors.New("cloudflared did not announce a public URL within 60s")
			default:
				return "", nil, errors.New("cloudflared announced URL but no edge connection within 60s (firewall blocking 7844/UDP+TCP?)")
			}
		}
	}

	stop := func() {
		cancel()
		_ = cmd.Wait()
	}
	return url, stop, nil
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
