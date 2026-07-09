package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"
)

// ---- raw shapes from `headscale ... -o json` (encoding/json over the
// protobuf-generated structs: snake_case fields, timestamps as seconds/nanos)

type pbTime struct {
	Seconds int64 `json:"seconds"`
	Nanos   int32 `json:"nanos"`
}

// RFC3339 returns the timestamp formatted for the UI, or "" for zero/nil.
func (t *pbTime) RFC3339() string {
	if t == nil || (t.Seconds == 0 && t.Nanos == 0) {
		return ""
	}
	return time.Unix(t.Seconds, int64(t.Nanos)).UTC().Format(time.RFC3339)
}

func (t *pbTime) Before(other time.Time) bool {
	if t == nil {
		return false
	}
	return time.Unix(t.Seconds, int64(t.Nanos)).Before(other)
}

type hsUser struct {
	ID          uint64  `json:"id"`
	Name        string  `json:"name"`
	CreatedAt   *pbTime `json:"created_at"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
}

type hsPreAuthKey struct {
	User       *hsUser  `json:"user"`
	ID         uint64   `json:"id"`
	Key        string   `json:"key"`
	Reusable   bool     `json:"reusable"`
	Ephemeral  bool     `json:"ephemeral"`
	Used       bool     `json:"used"`
	Expiration *pbTime  `json:"expiration"`
	CreatedAt  *pbTime  `json:"created_at"`
	AclTags    []string `json:"acl_tags"`
}

type hsNode struct {
	ID          uint64   `json:"id"`
	NodeKey     string   `json:"node_key"`
	IPAddresses []string `json:"ip_addresses"`
	Name        string   `json:"name"`
	User        *hsUser  `json:"user"`
	LastSeen    *pbTime  `json:"last_seen"`
	Expiry      *pbTime  `json:"expiry"`
	CreatedAt   *pbTime  `json:"created_at"`
	GivenName   string   `json:"given_name"`
	Online      bool     `json:"online"`
	Tags        []string `json:"tags"`
}

type hsVersion struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// ---- shapes returned by this plugin's API

type UserInfo struct {
	ID          uint64
	Name        string
	DisplayName string
	Email       string
	CreatedAt   string
}

type PreAuthKeyInfo struct {
	ID        uint64
	UserID    uint64
	UserName  string
	KeyPrefix string
	Reusable  bool
	Ephemeral bool
	Used      bool
	Expired   bool
	Expires   string
	CreatedAt string
}

// PreAuthKeyCreated is only returned by POST /preauthkeys; Key is the full
// secret, shown exactly once.
type PreAuthKeyCreated struct {
	PreAuthKeyInfo
	Key string
}

type NodeInfo struct {
	ID        uint64
	Name      string
	GivenName string
	User      string
	UserID    uint64
	IPs       []string
	Online    bool
	LastSeen  string
	Expiry    string
	Expired   bool
	CreatedAt string
	Tags      []string
}

// maskKey reduces a preauth key to a short identifying prefix. Keys are never
// listed back in full after creation.
func maskKey(key string) string {
	const keep = 10
	if len(key) <= keep {
		return key
	}
	return key[:keep] + "…"
}

func userInfo(u *hsUser) UserInfo {
	if u == nil {
		return UserInfo{}
	}
	return UserInfo{
		ID:          u.ID,
		Name:        u.Name,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		CreatedAt:   u.CreatedAt.RFC3339(),
	}
}

func preAuthKeyInfo(k hsPreAuthKey) PreAuthKeyInfo {
	info := PreAuthKeyInfo{
		ID:        k.ID,
		KeyPrefix: maskKey(k.Key),
		Reusable:  k.Reusable,
		Ephemeral: k.Ephemeral,
		Used:      k.Used,
		Expires:   k.Expiration.RFC3339(),
		CreatedAt: k.CreatedAt.RFC3339(),
	}
	if k.User != nil {
		info.UserID = k.User.ID
		info.UserName = k.User.Name
	}
	if k.Expiration != nil && k.Expiration.Before(time.Now()) {
		info.Expired = true
	}
	return info
}

func nodeInfo(n hsNode) NodeInfo {
	info := NodeInfo{
		ID:        n.ID,
		Name:      n.Name,
		GivenName: n.GivenName,
		IPs:       n.IPAddresses,
		Online:    n.Online,
		LastSeen:  n.LastSeen.RFC3339(),
		Expiry:    n.Expiry.RFC3339(),
		CreatedAt: n.CreatedAt.RFC3339(),
		Tags:      n.Tags,
	}
	if info.IPs == nil {
		info.IPs = []string{}
	}
	if info.Tags == nil {
		info.Tags = []string{}
	}
	if n.User != nil {
		info.User = n.User.Name
		info.UserID = n.User.ID
	}
	if n.Expiry != nil && !(n.Expiry.Seconds == 0 && n.Expiry.Nanos == 0) && n.Expiry.Before(time.Now()) {
		info.Expired = true
	}
	return info
}

// decodeList tolerates the CLI printing `null` for empty lists.
func decodeList[T any](data []byte) ([]T, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return []T{}, nil
	}
	var out []T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- CLI operations (argv arrays only; every variable token is validated
// upstream: IDs are digit-only strings, names/durations match allowlists)

func cliListUsers(ctx context.Context) ([]UserInfo, error) {
	data, err := runCLI(ctx, "users", "list")
	if err != nil {
		return nil, err
	}
	users, err := decodeList[hsUser](data)
	if err != nil {
		return nil, err
	}
	out := make([]UserInfo, 0, len(users))
	for _, u := range users {
		u := u
		out = append(out, userInfo(&u))
	}
	return out, nil
}

func cliCreateUser(ctx context.Context, name string) (UserInfo, error) {
	data, err := runCLI(ctx, "users", "create", name)
	if err != nil {
		return UserInfo{}, err
	}
	var u hsUser
	if err := json.Unmarshal(bytes.TrimSpace(data), &u); err != nil {
		return UserInfo{}, err
	}
	return userInfo(&u), nil
}

func cliDestroyUser(ctx context.Context, id string) error {
	_, err := runCLI(ctx, "users", "destroy", "--identifier", id)
	return err
}

func cliListPreAuthKeys(ctx context.Context, userID uint64) ([]PreAuthKeyInfo, error) {
	data, err := runCLI(ctx, "preauthkeys", "list")
	if err != nil {
		return nil, err
	}
	keys, err := decodeList[hsPreAuthKey](data)
	if err != nil {
		return nil, err
	}
	out := []PreAuthKeyInfo{}
	for _, k := range keys {
		if userID != 0 && (k.User == nil || k.User.ID != userID) {
			continue
		}
		out = append(out, preAuthKeyInfo(k))
	}
	return out, nil
}

func cliCreatePreAuthKey(ctx context.Context, userID uint64, reusable, ephemeral bool, expiration string) (PreAuthKeyCreated, error) {
	args := []string{
		"preauthkeys", "create",
		"--user", strconv.FormatUint(userID, 10),
		"--expiration", expiration,
	}
	if reusable {
		args = append(args, "--reusable")
	}
	if ephemeral {
		args = append(args, "--ephemeral")
	}
	data, err := runCLI(ctx, args...)
	if err != nil {
		return PreAuthKeyCreated{}, err
	}
	var k hsPreAuthKey
	if err := json.Unmarshal(bytes.TrimSpace(data), &k); err != nil {
		return PreAuthKeyCreated{}, err
	}
	return PreAuthKeyCreated{PreAuthKeyInfo: preAuthKeyInfo(k), Key: k.Key}, nil
}

func cliListNodes(ctx context.Context) ([]NodeInfo, error) {
	data, err := runCLI(ctx, "nodes", "list")
	if err != nil {
		return nil, err
	}
	nodes, err := decodeList[hsNode](data)
	if err != nil {
		return nil, err
	}
	out := make([]NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodeInfo(n))
	}
	return out, nil
}

func cliDeleteNode(ctx context.Context, id string) error {
	_, err := runCLI(ctx, "nodes", "delete", "--identifier", id)
	return err
}

func cliExpireNode(ctx context.Context, id string) error {
	_, err := runCLI(ctx, "nodes", "expire", "--identifier", id)
	return err
}

func cliVersion(ctx context.Context) (hsVersion, error) {
	// `headscale version` registers no global flags (--config/--force), so it
	// is invoked bare with just its own --output flag.
	var v hsVersion
	data, err := runCLIRaw(ctx, "version", "--output", "json")
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(bytes.TrimSpace(data), &v)
	return v, err
}
