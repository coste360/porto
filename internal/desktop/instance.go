package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coste360/porto/internal/crypto"
)

// InstanceTokenHeader carries the activation token of a second launch.
const InstanceTokenHeader = "Porto-Instance-Token"

// InstanceInfo is instance.json: where the running instance listens. The raw token is in instance.token (0600),
// the JSON keeps only its hash.
type InstanceInfo struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	TokenHash string `json:"token_hash"`
}

// WriteInstance mints a new activation token and writes instance.json and instance.token. It returns the token
// hash the runtime compares against.
func WriteInstance(p Paths, pid, port int) (tokenHash string, err error) {
	token, err := crypto.RandomToken(32)
	if err != nil {
		return "", err
	}
	info := InstanceInfo{PID: pid, Port: port, TokenHash: crypto.SHA256Hex(token)}
	b, err := json.Marshal(info)
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(p.InstanceToken(), []byte(token), 0o600); err != nil {
		return "", err
	}
	if err := writeFileAtomic(p.Instance(), b, 0o600); err != nil {
		return "", err
	}
	return info.TokenHash, nil
}

// RemoveInstance deletes instance.json and instance.token (normal stop).
func RemoveInstance(p Paths) {
	_ = os.Remove(p.Instance())
	_ = os.Remove(p.InstanceToken())
}

// ActivateRunning asks the instance that holds the lock to bring its window to the front
// (POST /api/desktop/v1/activate on its loopback port). client may be nil.
func ActivateRunning(ctx context.Context, p Paths, client *http.Client) error {
	b, err := os.ReadFile(p.Instance())
	if err != nil {
		return fmt.Errorf("desktop: read instance: %w", err)
	}
	var info InstanceInfo
	if err := json.Unmarshal(b, &info); err != nil || info.Port < 1 || info.Port > 65535 {
		return errors.New("desktop: instance file is not valid")
	}
	tok, err := os.ReadFile(p.InstanceToken())
	if err != nil {
		return fmt.Errorf("desktop: read instance token: %w", err)
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	u := "http://127.0.0.1:" + strconv.Itoa(info.Port) + "/api/desktop/v1/activate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set(InstanceTokenHeader, strings.TrimSpace(string(tok)))
	res, err := client.Do(req) //nolint:gosec // G704: fixed loopback URL of our own instance
	if err != nil {
		return fmt.Errorf("desktop: activate: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("desktop: activate: status %d", res.StatusCode)
	}
	return nil
}
