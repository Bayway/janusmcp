package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bayway/janusmcp/internal/atomicfile"
)

const daemonDefaultPort = 7332

type daemonMetadata struct {
	PID             int    `json:"pid"`
	Endpoint        string `json:"endpoint"`
	Token           string `json:"token"`
	Version         string `json:"version"`
	ConfigPath      string `json:"configPath"`
	ConfigModTimeNS int64  `json:"configModTimeNs"`
	ConfigSize      int64  `json:"configSize"`
	StartedAt       string `json:"startedAt"`
	LogPath         string `json:"logPath"`
}

type daemonHealthResponse struct {
	OK         bool   `json:"ok"`
	PID        int    `json:"pid"`
	Version    string `json:"version"`
	ConfigPath string `json:"configPath"`
	StartedAt  string `json:"startedAt"`
}

func daemonDirectory() (string, error) {
	if dir := os.Getenv("JANUS_DAEMON_DIR"); dir != "" {
		return filepath.Abs(dir)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "janusmcp"), nil
}

func daemonMetadataPath() (string, error) {
	dir, err := daemonDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.json"), nil
}

func loadDaemonMetadata() (*daemonMetadata, error) {
	path, err := daemonMetadataPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta daemonMetadata
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil, fmt.Errorf("parse daemon metadata: %w", err)
	}
	if meta.Endpoint == "" || meta.Token == "" {
		return nil, errors.New("daemon metadata is incomplete")
	}
	return &meta, nil
}

func writeDaemonMetadata(meta *daemonMetadata) error {
	path, err := daemonMetadataPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".daemon-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return atomicfile.Replace(tmpPath, path)
}

func removeDaemonMetadata() {
	if path, err := daemonMetadataPath(); err == nil {
		_ = os.Remove(path)
	}
}

func configSignature(path string) (int64, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	return info.ModTime().UnixNano(), info.Size()
}

func daemonToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func daemonHTTPClient(token string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: bearerTransport{token: token, base: http.DefaultTransport},
	}
}

func daemonHealth(meta *daemonMetadata, timeout time.Duration) (*daemonHealthResponse, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(meta.Endpoint, "/mcp")+"/_janus/health", nil)
	if err != nil {
		return nil, err
	}
	res, err := daemonHTTPClient(meta.Token, timeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon health returned %s", res.Status)
	}
	var health daemonHealthResponse
	if err := json.NewDecoder(res.Body).Decode(&health); err != nil {
		return nil, err
	}
	if !health.OK {
		return nil, errors.New("daemon is not healthy")
	}
	return &health, nil
}

func runDaemon(args []string) error {
	if len(args) == 0 {
		return cliErrf(exitUsage, "invalid_arguments", "usage: janusmcp daemon <start|stop|restart|status>")
	}
	switch args[0] {
	case "start":
		return runDaemonStart(args[1:])
	case "stop":
		return runDaemonStop()
	case "restart":
		if err := runDaemonStop(); err != nil {
			return err
		}
		return runDaemonStart(args[1:])
	case "status":
		return runDaemonStatus(args[1:])
	default:
		return cliErrf(exitUsage, "invalid_arguments", "unknown daemon command %q", args[0])
	}
}

func daemonPortFlag(args []string, name string) (int, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	port := fs.Int("port", daemonDefaultPort, "loopback port for the managed daemon")
	if err := fs.Parse(args); err != nil {
		return 0, cliErr(exitUsage, "invalid_arguments", err)
	}
	if len(fs.Args()) != 0 || *port < 1 || *port > 65535 {
		return 0, cliErrf(exitUsage, "invalid_port", "port must be between 1 and 65535")
	}
	return *port, nil
}

func runDaemonStart(args []string) error {
	port, err := daemonPortFlag(args, "daemon start")
	if err != nil {
		return err
	}
	cpath, _ := filepath.Abs(configPath())
	if existing, err := loadDaemonMetadata(); err == nil {
		if _, healthErr := daemonHealth(existing, 750*time.Millisecond); healthErr == nil {
			if existing.ConfigPath == cpath && existing.Version == version && existing.Endpoint == fmt.Sprintf("http://127.0.0.1:%d/mcp", port) {
				fmt.Printf("daemon already running: %s (pid %d)\n", existing.Endpoint, existing.PID)
				return nil
			}
			return cliErrf(exitSelection, "daemon_mismatch", "daemon is running with different settings; run 'janusmcp daemon restart --port %d'", port)
		}
		removeDaemonMetadata()
	}

	dir, err := daemonDirectory()
	if err != nil {
		return cliErr(exitSelection, "daemon_directory", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return cliErr(exitSelection, "daemon_directory", err)
	}
	logPath := filepath.Join(dir, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return cliErr(exitSelection, "daemon_log", err)
	}
	defer logFile.Close()
	token, err := daemonToken()
	if err != nil {
		return cliErr(exitGeneric, "token_generation", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return cliErr(exitGeneric, "executable_path", err)
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	modTime, size := configSignature(cpath)
	startedAt := time.Now().UTC().Format(time.RFC3339)
	cmd := exec.Command(exe, "_daemon-serve")
	cmd.Env = append(os.Environ(),
		"JANUS_CONFIG="+cpath,
		"JANUS_DAEMON_TOKEN="+token,
		"JANUS_DAEMON_STARTED_AT="+startedAt,
		"JANUS_HTTP_PORT="+strconv.Itoa(port),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	detachDaemonCommand(cmd)
	if err := cmd.Start(); err != nil {
		return cliErr(exitUpstream, "daemon_start", err)
	}
	meta := &daemonMetadata{
		PID:             cmd.Process.Pid,
		Endpoint:        endpoint,
		Token:           token,
		Version:         version,
		ConfigPath:      cpath,
		ConfigModTimeNS: modTime,
		ConfigSize:      size,
		StartedAt:       startedAt,
		LogPath:         logPath,
	}
	if err := writeDaemonMetadata(meta); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return cliErr(exitSelection, "daemon_metadata", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := daemonHealth(meta, 500*time.Millisecond); err == nil {
			_ = cmd.Process.Release()
			fmt.Printf("daemon running: %s (pid %d)\n", endpoint, meta.PID)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	removeDaemonMetadata()
	return cliErrf(exitUpstream, "daemon_start", "daemon did not become ready within 10s; see %s", logPath)
}

func runDaemonStop() error {
	meta, err := loadDaemonMetadata()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("daemon is not running")
			return nil
		}
		removeDaemonMetadata()
		fmt.Println("removed stale daemon metadata")
		return nil
	}
	if _, err := daemonHealth(meta, 750*time.Millisecond); err != nil {
		removeDaemonMetadata()
		fmt.Println("daemon is not running; removed stale metadata")
		return nil
	}
	url := strings.TrimSuffix(meta.Endpoint, "/mcp") + "/_janus/shutdown"
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	res, err := daemonHTTPClient(meta.Token, 2*time.Second).Do(req)
	if err != nil {
		return cliErr(exitUpstream, "daemon_stop", err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		return cliErrf(exitUpstream, "daemon_stop", "daemon shutdown returned %s", res.Status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := daemonHealth(meta, 200*time.Millisecond); err != nil {
			removeDaemonMetadata()
			fmt.Println("daemon stopped")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cliErr(exitUpstream, "daemon_stop", errors.New("daemon did not stop within 5s"))
}

func runDaemonStatus(args []string) error {
	fs := flag.NewFlagSet("daemon status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return jsonIf(cliErr(exitUsage, "invalid_arguments", err), *asJSON)
	}
	meta, err := loadDaemonMetadata()
	if err != nil {
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "running": false})
		}
		fmt.Println("daemon: stopped")
		return nil
	}
	health, healthErr := daemonHealth(meta, 750*time.Millisecond)
	modTime, size := configSignature(meta.ConfigPath)
	configMatches := modTime == meta.ConfigModTimeNS && size == meta.ConfigSize
	status := map[string]any{
		"ok":            true,
		"running":       healthErr == nil,
		"pid":           meta.PID,
		"endpoint":      meta.Endpoint,
		"version":       meta.Version,
		"configPath":    meta.ConfigPath,
		"configMatches": configMatches,
		"startedAt":     meta.StartedAt,
		"logPath":       meta.LogPath,
	}
	if health != nil {
		status["pid"] = health.PID
	}
	if healthErr != nil {
		status["error"] = healthErr.Error()
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(status)
	}
	state := "running"
	if healthErr != nil {
		state = "stale"
	}
	fmt.Printf("daemon: %s\nendpoint: %s\npid: %d\nconfig: %s\nconfig current: %t\nlog: %s\n", state, meta.Endpoint, meta.PID, meta.ConfigPath, configMatches, meta.LogPath)
	return nil
}

func daemonAuthorized(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(got) != len(token) || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"ok":false,"error":"unauthorized"}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// runDaemonChild is an internal re-exec target. It deliberately has no public
// help entry and accepts its configuration only through the parent process.
func runDaemonChild() error {
	token := os.Getenv("JANUS_DAEMON_TOKEN")
	if token == "" {
		return errors.New("missing daemon token")
	}
	port := envOr("JANUS_HTTP_PORT", strconv.Itoa(daemonDefaultPort))
	runtime, err := newBrokerRuntime()
	if err != nil {
		return err
	}
	defer runtime.core.Manager.CloseAll()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mux := http.NewServeMux()
	mux.Handle("/mcp", daemonAuthorized(token, runtime.core.HTTPHandler()))
	mux.Handle("/_janus/health", daemonAuthorized(token, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(daemonHealthResponse{
			OK:         true,
			PID:        os.Getpid(),
			Version:    version,
			ConfigPath: runtime.configPath,
			StartedAt:  os.Getenv("JANUS_DAEMON_STARTED_AT"),
		})
	})))
	mux.Handle("/_janus/shutdown", daemonAuthorized(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		go cancel()
	})))
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
