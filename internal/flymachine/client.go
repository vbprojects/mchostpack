package flymachine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hostpack/hostpack/internal/config"
)

const defaultAPIBase = "https://api.machines.dev"

// Client reconciles the resources of the Fly Machine that is running
// hostpackd. Updates happen before a Minecraft child is launched and cause Fly
// to reboot the VM; the persisted lifecycle state resumes the requested pack.
type Client struct {
	baseURL, app, machineID, token string
	httpClient                     *http.Client
}

func New(baseURL, app, machineID, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(app) == "" || strings.TrimSpace(machineID) == "" || strings.TrimSpace(token) == "" {
		return nil, errors.New("Fly resize requires API base, app, Machine ID, and token")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid Fly Machines API base %q", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{baseURL: baseURL, app: strings.TrimSpace(app), machineID: strings.TrimSpace(machineID), token: strings.TrimSpace(token), httpClient: httpClient}, nil
}

// FromEnv returns nil outside Fly. On Fly it fails closed when the app-scoped
// deploy token is absent, since starting a large pack in an undersized VM is
// unsafe and usually corrupts the user experience with an OOM kill.
func FromEnv() (*Client, error) {
	machineID := strings.TrimSpace(os.Getenv("FLY_MACHINE_ID"))
	if machineID == "" {
		return nil, nil
	}
	app := strings.TrimSpace(os.Getenv("HOSTPACK_FLY_APP"))
	if app == "" {
		app = strings.TrimSpace(os.Getenv("FLY_APP_NAME"))
	}
	token := strings.TrimSpace(os.Getenv("HOSTPACK_FLY_API_TOKEN"))
	if app == "" || token == "" {
		return nil, errors.New("HOSTPACK_FLY_APP and HOSTPACK_FLY_API_TOKEN are required for per-pack Fly sizing")
	}
	baseURL := strings.TrimSpace(os.Getenv("HOSTPACK_FLY_API_BASE"))
	if baseURL == "" {
		baseURL = defaultAPIBase
	}
	return New(baseURL, app, machineID, token, nil)
}

func (c *Client) Check(ctx context.Context) error {
	_, err := c.get(ctx)
	return err
}

// Ensure returns true after submitting a resource change. Fly reboots the
// current Machine as part of that update, so callers must not launch Java when
// changed is true.
func (c *Client) Ensure(ctx context.Context, pack config.Pack) (bool, error) {
	machine, err := c.get(ctx)
	if err != nil {
		return false, err
	}
	guest, ok := machine.Config["guest"].(map[string]any)
	if !ok {
		return false, errors.New("Fly Machine config has no guest resources")
	}
	memory, memoryOK := integer(guest["memory_mb"])
	cpus, cpusOK := integer(guest["cpus"])
	cpuKind, kindOK := guest["cpu_kind"].(string)
	if !memoryOK || !cpusOK || !kindOK {
		return false, errors.New("Fly Machine guest resources are malformed")
	}
	if memory == pack.MachineMemoryMB && cpus == pack.MachineCPUs && cpuKind == "shared" {
		return false, nil
	}
	guest["memory_mb"] = pack.MachineMemoryMB
	guest["cpus"] = pack.MachineCPUs
	guest["cpu_kind"] = "shared"

	payload := struct {
		Config     map[string]any `json:"config"`
		SkipLaunch bool           `json:"skip_launch"`
	}{Config: machine.Config, SkipLaunch: false}
	body, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	req, err := c.request(ctx, http.MethodPost, body)
	if err != nil {
		return false, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("update Fly Machine resources: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return false, responseError("update Fly Machine resources", resp)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return true, nil
}

type machineResponse struct {
	Config map[string]any `json:"config"`
}

func (c *Client) get(ctx context.Context) (machineResponse, error) {
	req, err := c.request(ctx, http.MethodGet, nil)
	if err != nil {
		return machineResponse{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return machineResponse{}, fmt.Errorf("read Fly Machine resources: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return machineResponse{}, responseError("read Fly Machine resources", resp)
	}
	var machine machineResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, 4<<20))
	if err := dec.Decode(&machine); err != nil {
		return machineResponse{}, fmt.Errorf("decode Fly Machine resources: %w", err)
	}
	if machine.Config == nil {
		return machineResponse{}, errors.New("Fly Machine response has no config")
	}
	return machine, nil
}

func (c *Client) request(ctx context.Context, method string, body []byte) (*http.Request, error) {
	endpoint := c.baseURL + "/v1/apps/" + url.PathEscape(c.app) + "/machines/" + url.PathEscape(c.machineID)
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "hostpackd/1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func responseError(action string, resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	detail := strings.TrimSpace(string(b))
	if detail == "" {
		detail = resp.Status
	}
	return fmt.Errorf("%s: %s: %s", action, resp.Status, detail)
}

func integer(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}
