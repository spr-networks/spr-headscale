package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var HeadscaleBin = "headscale"

// Daemon supervises the headscale server child process.
type Daemon struct {
	mtx        sync.Mutex
	cmd        *exec.Cmd
	generation int
	stopped    bool
}

var gDaemon = &Daemon{}

// getContainerIP returns the container's IPv4 address on eth0
// (the spr-headscale docker bridge).
func getContainerIP() string {
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

func listenIP() string {
	if ip := getContainerIP(); ip != "" {
		return ip
	}
	// dev / test fallback
	return "127.0.0.1"
}

// Start renders config.yaml and launches `headscale serve`, restarting it with
// a delay if it dies unexpectedly.
func (d *Daemon) Start() error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	return d.startLocked()
}

func (d *Daemon) startLocked() error {
	Configmtx.RLock()
	cfg := gConfig
	Configmtx.RUnlock()

	if err := os.MkdirAll(filepath.Dir(HeadscaleDBPath), 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(HeadscaleSocketDir, 0700); err != nil {
		return err
	}
	if err := writeHeadscaleConfig(cfg, listenIP()); err != nil {
		return err
	}

	cmd := exec.Command(HeadscaleBin, "serve", "--config", HeadscaleConfigFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting headscale: %v", err)
	}
	d.cmd = cmd
	d.stopped = false
	d.generation++
	gen := d.generation
	log.Printf("headscale started (pid %d)", cmd.Process.Pid)

	go enforceKeyPerms()
	go func() {
		err := cmd.Wait()
		d.mtx.Lock()
		if d.generation != gen || d.stopped {
			d.mtx.Unlock()
			return
		}
		d.cmd = nil
		d.mtx.Unlock()
		log.Printf("headscale exited unexpectedly: %v; restarting in 5s", err)
		time.Sleep(5 * time.Second)
		d.mtx.Lock()
		defer d.mtx.Unlock()
		if d.generation == gen && !d.stopped {
			if err := d.startLocked(); err != nil {
				log.Printf("headscale restart failed: %v", err)
			}
		}
	}()
	return nil
}

func (d *Daemon) stopLocked() {
	d.stopped = true
	d.generation++
	if d.cmd != nil && d.cmd.Process != nil {
		proc := d.cmd.Process
		proc.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			d.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			proc.Kill()
		}
	}
	d.cmd = nil
}

// Restart stops headscale (if running), regenerates config.yaml and starts it again.
func (d *Daemon) Restart() error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.stopLocked()
	return d.startLocked()
}

// Stop terminates headscale for plugin shutdown.
func (d *Daemon) Stop() {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.stopLocked()
}

// Running reports whether the headscale child process is alive.
func (d *Daemon) Running() bool {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	return d.cmd != nil && d.cmd.Process != nil && d.cmd.ProcessState == nil
}

// enforceKeyPerms tightens the noise private key mode once headscale has
// generated it (headscale writes it 0600 itself; this is belt and braces).
func enforceKeyPerms() {
	for i := 0; i < 30; i++ {
		if _, err := os.Stat(NoiseKeyPath); err == nil {
			os.Chmod(NoiseKeyPath, 0600)
			return
		}
		time.Sleep(time.Second)
	}
}

// runCLI executes the headscale CLI (talking to the daemon over its unix
// socket) with machine-readable output. args must be fixed strings or
// validated tokens — never raw user input.
func runCLI(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string(nil), args...)
	full = append(full, "--config", HeadscaleConfigFile, "--output", "json", "--force")
	return runCLIRaw(ctx, full...)
}

// runCLIRaw executes the headscale binary with exactly the given argv.
func runCLIRaw(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, HeadscaleBin, args...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("headscale %s: %s", args[0], msg)
	}
	return stdout.Bytes(), nil
}
