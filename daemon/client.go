package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

const DefaultSocketPath = "/var/run/bottle/daemon.sock"

type DaemonClient interface {
	StartAnalysis(req StartAnalysisRequest) (string, error)
	StopAnalysis(id string) error
	Inspect(id string) (WorkerDetails, error)
	List() ([]WorkerStatus, error)
	CleanupInactive() (int, error)
}

type Client struct {
	socketPath string
	timeout    time.Duration
}

func NewClient(socketPath string) DaemonClient {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	return &Client{
		socketPath: socketPath,
		timeout:    30 * time.Second,
	}
}

func (c *Client) send(request IPCRequest, response interface{}) error {
	conn, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	var resp IPCResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		return fmt.Errorf("daemon request failed")
	}
	if response != nil && resp.Data != nil {
		data, err := json.Marshal(resp.Data)
		if err != nil {
			return fmt.Errorf("marshal response payload: %w", err)
		}
		if err := json.Unmarshal(data, response); err != nil {
			return fmt.Errorf("unmarshal response payload: %w", err)
		}
	}
	return nil
}

func (c *Client) StartAnalysis(req StartAnalysisRequest) (string, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := c.send(IPCRequest{Command: CommandStart, Payload: payload}, &result); err != nil {
		return "", err
	}
	return result.ID, nil
}

func (c *Client) StopAnalysis(id string) error {
	return c.send(IPCRequest{Command: CommandStop, ID: id}, nil)
}

func (c *Client) List() ([]WorkerStatus, error) {
	var statuses []WorkerStatus
	if err := c.send(IPCRequest{Command: CommandList}, &statuses); err != nil {
		return nil, err
	}
	return statuses, nil
}

func (c *Client) Inspect(id string) (WorkerDetails, error) {
	var detail WorkerDetails
	if err := c.send(IPCRequest{Command: CommandInspect, ID: id}, &detail); err != nil {
		return WorkerDetails{}, err
	}
	return detail, nil
}

func (c *Client) CleanupInactive() (int, error) {
	var resp struct {
		Removed int `json:"removed"`
	}
	if err := c.send(IPCRequest{Command: CommandCleanup}, &resp); err != nil {
		return 0, err
	}
	return resp.Removed, nil
}
