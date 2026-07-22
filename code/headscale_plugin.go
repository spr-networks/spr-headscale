package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
)

var UNIX_PLUGIN_LISTENER = "/run/spr-krun-plugin/spr-headscale.sock"

// PinnedHeadscaleVersion is stamped at build time via -ldflags from
// HEADSCALE_VERSION in reproducible.env.
var PinnedHeadscaleVersion = "unknown"

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("encode failed:", err)
	}
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"Error": msg})
}

// ---- /status

type StatusResponse struct {
	Running       bool
	Version       string
	Commit        string
	PinnedVersion string
	ServerURL     string
	ListenAddr    string
	ContainerIP   string
	MagicDNS      bool
	BaseDomain    string
	DERPEnabled   bool
	LastError     string
	RecentLogs    string
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	Configmtx.RLock()
	cfg := gConfig
	Configmtx.RUnlock()

	ip := listenIP()
	running := gDaemon.Running()
	lastError, recentLogs := gDaemon.Diagnostics()
	status := StatusResponse{
		Running:       running,
		PinnedVersion: PinnedHeadscaleVersion,
		ListenAddr:    ip + ":8080",
		ContainerIP:   ip,
		MagicDNS:      cfg.MagicDNS,
		BaseDomain:    cfg.BaseDomain,
		DERPEnabled:   cfg.DERPEnabled,
		LastError:     lastError,
		RecentLogs:    recentLogs,
	}
	status.ServerURL = cfg.ServerURL
	if status.ServerURL == "" {
		status.ServerURL = "http://" + ip + ":8080"
	}
	if running {
		if v, err := cliVersion(r.Context()); err == nil {
			status.Version = v.Version
			status.Commit = v.Commit
		}
	}
	jsonOK(w, status)
}

// ---- /config

func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	Configmtx.RLock()
	defer Configmtx.RUnlock()
	jsonOK(w, gConfig)
}

func handlePutConfig(w http.ResponseWriter, r *http.Request) {
	cfg := defaultConfig()
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		jsonError(w, "invalid JSON: "+err.Error(), 400)
		return
	}
	if err := validateConfig(&cfg); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	Configmtx.Lock()
	gConfig = cfg
	err := writeConfigLocked()
	Configmtx.Unlock()
	if err != nil {
		jsonError(w, "failed to persist config: "+err.Error(), 500)
		return
	}

	// apply: regenerate config.yaml and restart headscale
	if err := gDaemon.Restart(); err != nil {
		jsonError(w, "config saved but headscale restart failed: "+err.Error(), 500)
		return
	}
	jsonOK(w, cfg)
}

// ---- /users

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := cliListUsers(r.Context())
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, users)
}

type createUserRequest struct {
	Name string
}

func handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON: "+err.Error(), 400)
		return
	}
	if err := validateUsername(req.Name); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	user, err := cliCreateUser(r.Context(), req.Name)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, user)
}

func handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := validateID(id); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if err := cliDestroyUser(r.Context(), id); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]bool{"Deleted": true})
}

// ---- /preauthkeys

func handleListPreAuthKeys(w http.ResponseWriter, r *http.Request) {
	var userID uint64
	if uq := r.URL.Query().Get("user"); uq != "" {
		if err := validateID(uq); err != nil {
			jsonError(w, "invalid user id", 400)
			return
		}
		userID, _ = strconv.ParseUint(uq, 10, 64)
	}
	keys, err := cliListPreAuthKeys(r.Context(), userID)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, keys)
}

type createPreAuthKeyRequest struct {
	User       uint64
	Reusable   bool
	Ephemeral  bool
	Expiration string
}

func handleCreatePreAuthKey(w http.ResponseWriter, r *http.Request) {
	var req createPreAuthKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON: "+err.Error(), 400)
		return
	}
	if req.User == 0 {
		jsonError(w, "missing user id", 400)
		return
	}
	if req.Expiration == "" {
		req.Expiration = "1h"
	}
	if err := validateExpiration(req.Expiration); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	// The full key is returned exactly once, in this response.
	created, err := cliCreatePreAuthKey(r.Context(), req.User, req.Reusable, req.Ephemeral, req.Expiration)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, created)
}

// ---- /nodes

func handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := cliListNodes(r.Context())
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, nodes)
}

func handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := validateID(id); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if err := cliDeleteNode(r.Context(), id); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]bool{"Deleted": true})
}

func handleExpireNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := validateID(id); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if err := cliExpireNode(r.Context(), id); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]bool{"Expired": true})
}

// ---- /restart

func handleRestart(w http.ResponseWriter, r *http.Request) {
	if err := gDaemon.Restart(); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]bool{"Restarted": true})
}

// ---- UI (served from /ui, injected by the SPR host as iframe srcDoc)

type spaHandler struct {
	staticPath string
	indexPath  string
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path = filepath.Join(h.staticPath, path)
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

func logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
}

func setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", handleStatus)
	mux.HandleFunc("GET /config", handleGetConfig)
	mux.HandleFunc("PUT /config", handlePutConfig)
	mux.HandleFunc("GET /users", handleListUsers)
	mux.HandleFunc("POST /users", handleCreateUser)
	mux.HandleFunc("DELETE /users/{id}", handleDeleteUser)
	mux.HandleFunc("GET /preauthkeys", handleListPreAuthKeys)
	mux.HandleFunc("POST /preauthkeys", handleCreatePreAuthKey)
	mux.HandleFunc("GET /nodes", handleListNodes)
	mux.HandleFunc("DELETE /nodes/{id}", handleDeleteNode)
	mux.HandleFunc("POST /nodes/{id}/expire", handleExpireNode)
	mux.HandleFunc("POST /restart", handleRestart)
	mux.HandleFunc("GET /topology", handleGetTopology)
	mux.Handle("/", spaHandler{staticPath: "/ui", indexPath: "index.html"})
	return mux
}

func main() {
	if err := loadConfig(); err != nil {
		log.Println("config load failed (using defaults):", err)
	}

	if err := gDaemon.Start(); err != nil {
		// keep the API up so the UI can show the error and fix the config
		log.Println("headscale start failed:", err)
	}

	os.Remove(UNIX_PLUGIN_LISTENER)
	listener, err := net.Listen("unix", UNIX_PLUGIN_LISTENER)
	if err != nil {
		panic(err)
	}
	if err := os.Chmod(UNIX_PLUGIN_LISTENER, 0770); err != nil {
		panic(err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		gDaemon.Stop()
		listener.Close()
		os.Exit(0)
	}()

	server := http.Server{Handler: logRequest(setupRoutes())}
	server.Serve(listener)
}
