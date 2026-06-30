package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// APIClient handles HTTP requests to the remote runqy server
type APIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewAPIClient creates a new API client for remote server communication
func NewAPIClient() *APIClient {
	return &APIClient{
		baseURL: strings.TrimSuffix(GetServerURL(), "/"),
		apiKey:  GetAPIKey(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// request makes an HTTP request to the server
func (c *APIClient) request(method, path string, body interface{}) ([]byte, error) {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GET makes a GET request
func (c *APIClient) GET(path string) ([]byte, error) {
	return c.request("GET", path, nil)
}

// POST makes a POST request
func (c *APIClient) POST(path string, body interface{}) ([]byte, error) {
	return c.request("POST", path, body)
}

// DELETE makes a DELETE request
func (c *APIClient) DELETE(path string) ([]byte, error) {
	return c.request("DELETE", path, nil)
}

// PUT makes a PUT request
func (c *APIClient) PUT(path string, body interface{}) ([]byte, error) {
	return c.request("PUT", path, body)
}

// --- Queue API Types ---

// QueueInfo represents queue information from the API
type QueueInfo struct {
	Queue       string `json:"queue"`
	Pending     int    `json:"pending"`
	Active      int    `json:"active"`
	Scheduled   int    `json:"scheduled"`
	Retry       int    `json:"retry"`
	Archived    int    `json:"archived"`
	Completed   int    `json:"completed"`
	Paused      bool   `json:"paused"`
	MemoryUsage int64  `json:"memory_usage"`
}

// QueueListResponse is the response for listing queues
type QueueListResponse struct {
	Queues []QueueInfo `json:"queues"`
}

// ListQueues fetches all queues from the server
func (c *APIClient) ListQueues() ([]QueueInfo, error) {
	data, err := c.GET("/api/queues")
	if err != nil {
		return nil, err
	}

	var resp QueueListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Queues, nil
}

// GetQueueInfo fetches info for a specific queue
func (c *APIClient) GetQueueInfo(queueName string) (*QueueInfo, error) {
	data, err := c.GET("/api/queues/" + queueName)
	if err != nil {
		return nil, err
	}

	var info QueueInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &info, nil
}

// PauseQueue pauses a queue
func (c *APIClient) PauseQueue(queueName string) error {
	_, err := c.POST("/api/queues/"+queueName+"/pause", nil)
	return err
}

// UnpauseQueue unpauses a queue
func (c *APIClient) UnpauseQueue(queueName string) error {
	_, err := c.POST("/api/queues/"+queueName+"/unpause", nil)
	return err
}

// --- Task API Types ---

// TaskInfo represents task information from the API
type TaskInfo struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Queue         string          `json:"queue"`
	State         string          `json:"state"`
	Payload       json.RawMessage `json:"payload"`
	MaxRetry      int             `json:"max_retry"`
	Retried       int             `json:"retried"`
	LastErr       string          `json:"last_err,omitempty"`
	Timeout       string          `json:"timeout"`
	Deadline      string          `json:"deadline,omitempty"`
	NextProcessAt string          `json:"next_process_at,omitempty"`
	CompletedAt   string          `json:"completed_at,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
}

// TaskListResponse is the response for listing tasks
type TaskListResponse struct {
	Tasks []TaskInfo `json:"tasks"`
}

// EnqueueRequest is the request body for enqueueing a task
type EnqueueRequest struct {
	Queue   string          `json:"queue"`
	Timeout int64           `json:"timeout"`
	Data    json.RawMessage `json:"data"`
}

// EnqueueResponse is the response for enqueueing a task
type EnqueueResponse struct {
	Info TaskInfo `json:"info"`
}

// EnqueueTask enqueues a new task
func (c *APIClient) EnqueueTask(queue string, payload json.RawMessage, timeout int64) (*TaskInfo, error) {
	req := EnqueueRequest{
		Queue:   queue,
		Timeout: timeout,
		Data:    payload,
	}

	data, err := c.POST("/queue/add", req)
	if err != nil {
		return nil, err
	}

	var resp EnqueueResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp.Info, nil
}

// ListTasks lists tasks in a queue by state
func (c *APIClient) ListTasks(queueName, state string, limit int) ([]TaskInfo, error) {
	path := fmt.Sprintf("/api/queues/%s/tasks?state=%s&limit=%d", queueName, state, limit)
	data, err := c.GET(path)
	if err != nil {
		return nil, err
	}

	var resp TaskListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Tasks, nil
}

// GetTask gets task info
func (c *APIClient) GetTask(queueName, taskID string) (*TaskInfo, error) {
	data, err := c.GET(fmt.Sprintf("/api/queues/%s/tasks/%s", queueName, taskID))
	if err != nil {
		return nil, err
	}

	var info TaskInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &info, nil
}

// CancelTask cancels a task
func (c *APIClient) CancelTask(taskID string) error {
	_, err := c.POST("/api/tasks/"+taskID+"/cancel", nil)
	return err
}

// DeleteTask deletes a task
func (c *APIClient) DeleteTask(queueName, taskID string) error {
	_, err := c.DELETE(fmt.Sprintf("/api/queues/%s/tasks/%s", queueName, taskID))
	return err
}

// --- Worker API Types ---

// WorkerInfoAPI represents worker information from the API
type WorkerInfoAPI struct {
	WorkerID    string `json:"worker_id"`
	StartedAt   int64  `json:"started_at"`
	LastBeat    int64  `json:"last_beat"`
	Concurrency int    `json:"concurrency"`
	Queues      string `json:"queues"`
	Status      string `json:"status"`
	IsStale     bool   `json:"is_stale"`
}

// WorkersResponse is the API response for listing workers
type WorkersResponse struct {
	Workers []WorkerInfoAPI `json:"workers"`
	Count   int             `json:"count"`
}

// ListWorkers fetches all workers from the server
func (c *APIClient) ListWorkers() ([]WorkerInfoAPI, error) {
	data, err := c.GET("/workers")
	if err != nil {
		return nil, err
	}

	var resp WorkersResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Workers, nil
}

// GetWorker fetches a specific worker's info
func (c *APIClient) GetWorker(workerID string) (*WorkerInfoAPI, error) {
	data, err := c.GET("/workers/" + workerID)
	if err != nil {
		return nil, err
	}

	var info WorkerInfoAPI
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &info, nil
}

// --- Config API Types ---

// QueueConfigAPI represents queue configuration from the API
type QueueConfigAPI struct {
	Name       string                `json:"name"`
	Priority   int                   `json:"priority"`
	Deployment *DeploymentConfigAPI  `json:"deployment,omitempty"`
}

// DeploymentConfigAPI represents deployment configuration
type DeploymentConfigAPI struct {
	GitURL             string   `json:"git_url"`
	Branch             string   `json:"branch"`
	CodePath           string   `json:"code_path,omitempty"`
	StartupCmd         string   `json:"startup_cmd"`
	Mode               string   `json:"mode,omitempty"`
	StartupTimeoutSecs int      `json:"startup_timeout_secs,omitempty"`
	RedisStorage       *bool    `json:"redis_storage,omitempty"`
	Vaults             []string `json:"vaults,omitempty"`
	GitToken           string   `json:"git_token,omitempty"`
}

// ConfigListResponse is the response for listing configs
type ConfigListResponse struct {
	Queues []QueueSummaryAPI `json:"queues"`
	Count  int               `json:"count"`
}

// QueueSummaryAPI is a lightweight queue config summary
type QueueSummaryAPI struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

// ListConfigs fetches all queue configurations
func (c *APIClient) ListConfigs() ([]QueueSummaryAPI, error) {
	data, err := c.GET("/workers/queues")
	if err != nil {
		return nil, err
	}

	var resp ConfigListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Queues, nil
}

// ReloadResponse is the response for reloading configs
type ReloadResponse struct {
	Reloaded  []string `json:"reloaded"`
	Errors    []string `json:"errors"`
	Timestamp int64    `json:"timestamp"`
}

// ReloadConfigs triggers config reload on the server
func (c *APIClient) ReloadConfigs() (*ReloadResponse, error) {
	data, err := c.POST("/workers/reload", nil)
	if err != nil {
		return nil, err
	}

	var resp ReloadResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

// CreateQueueRequest is the request body for creating a queue
type CreateQueueRequest struct {
	Name       string               `json:"name"`
	Priority   int                  `json:"priority"`
	Deployment *DeploymentConfigAPI `json:"deployment,omitempty"`
}

// CreateQueueResponse is the response for queue creation
type CreateQueueResponse struct {
	Queue   *QueueConfigAPI `json:"queue"`
	Message string          `json:"message"`
}

// CreateQueue creates a new queue configuration
func (c *APIClient) CreateQueue(req *CreateQueueRequest, force bool) (*CreateQueueResponse, error) {
	path := "/workers/queues"
	if force {
		path = "/workers/queues?force=true"
	}

	data, err := c.POST(path, req)
	if err != nil {
		return nil, err
	}

	var resp CreateQueueResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

// DeleteQueue deletes a queue configuration
func (c *APIClient) DeleteQueue(queueName string) error {
	_, err := c.DELETE("/workers/queues/" + queueName)
	return err
}

// --- Vault API Types ---

// VaultSummaryAPI represents a vault summary from the API
type VaultSummaryAPI struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	EntryCount  int    `json:"entry_count"`
}

// VaultEntryViewAPI represents a vault entry from the API
type VaultEntryViewAPI struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	IsSecret  bool   `json:"is_secret"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// VaultDetailAPI represents a vault with entries from the API
type VaultDetailAPI struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Entries     []VaultEntryViewAPI `json:"entries"`
	CreatedAt   string              `json:"created_at"`
	UpdatedAt   string              `json:"updated_at"`
}

// VaultsListResponseAPI is the response for listing vaults
type VaultsListResponseAPI struct {
	Vaults []VaultSummaryAPI `json:"vaults"`
	Count  int               `json:"count"`
}

// VaultEntriesResponseAPI is the response for listing vault entries
type VaultEntriesResponseAPI struct {
	Entries []VaultEntryViewAPI `json:"entries"`
	Count   int                 `json:"count"`
}

// CreateVaultRequestAPI is the request body for creating a vault
type CreateVaultRequestAPI struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SetEntryRequestAPI is the request body for setting a vault entry
type SetEntryRequestAPI struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret *bool  `json:"is_secret,omitempty"`
}

// ListVaultsAPI fetches all vaults from the server
func (c *APIClient) ListVaultsAPI() ([]VaultSummaryAPI, error) {
	data, err := c.GET("/api/vaults")
	if err != nil {
		return nil, err
	}

	var resp VaultsListResponseAPI
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Vaults, nil
}

// GetVaultAPI fetches a vault with its entries
func (c *APIClient) GetVaultAPI(name string) (*VaultDetailAPI, error) {
	data, err := c.GET("/api/vaults/" + name)
	if err != nil {
		return nil, err
	}

	var detail VaultDetailAPI
	if err := json.Unmarshal(data, &detail); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &detail, nil
}

// CreateVaultAPI creates a new vault
func (c *APIClient) CreateVaultAPI(name, description string) error {
	req := CreateVaultRequestAPI{
		Name:        name,
		Description: description,
	}

	_, err := c.POST("/api/vaults", req)
	return err
}

// DeleteVaultAPI deletes a vault
func (c *APIClient) DeleteVaultAPI(name string) error {
	_, err := c.DELETE("/api/vaults/" + name)
	return err
}

// SetEntryAPI sets a vault entry
func (c *APIClient) SetEntryAPI(vaultName, key, value string, isSecret *bool) error {
	req := SetEntryRequestAPI{
		Key:      key,
		Value:    value,
		IsSecret: isSecret,
	}

	_, err := c.POST("/api/vaults/"+vaultName+"/entries", req)
	return err
}

// GetEntriesAPI fetches vault entries
func (c *APIClient) GetEntriesAPI(vaultName string) ([]VaultEntryViewAPI, error) {
	data, err := c.GET("/api/vaults/" + vaultName + "/entries")
	if err != nil {
		return nil, err
	}

	var resp VaultEntriesResponseAPI
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Entries, nil
}

// DeleteEntryAPI deletes a vault entry
func (c *APIClient) DeleteEntryAPI(vaultName, key string) error {
	_, err := c.DELETE("/api/vaults/" + vaultName + "/entries/" + key)
	return err
}

// --- Autoscale API Types ---

// AutoscaleInstanceAPI represents a tracked autoscaler instance from the API.
type AutoscaleInstanceAPI struct {
	InstanceID      string  `json:"instance_id"`
	Provider        string  `json:"provider"`
	Queue           string  `json:"queue"`
	Status          string  `json:"status"`
	WorkerID        string  `json:"worker_id"`
	Protected       bool    `json:"protected"`
	PricePerHour    float64 `json:"price_per_hour"`
	CostAccumulated float64 `json:"cost_accumulated"`
}

// AutoscaleStatusResponseAPI is the response for GET /api/autoscale/status.
type AutoscaleStatusResponseAPI struct {
	Instances []AutoscaleInstanceAPI `json:"instances"`
	Count     int                    `json:"count"`
	TotalCost float64                `json:"total_cost"`
}

// AutoscaleProviderAPI represents a provider config from the API (config masked).
type AutoscaleProviderAPI struct {
	Name         string          `json:"name"`
	ProviderType string          `json:"provider_type"`
	Config       json.RawMessage `json:"config"`
	Enabled      bool            `json:"enabled"`
	CreatedAt    string          `json:"created_at,omitempty"`
	UpdatedAt    string          `json:"updated_at,omitempty"`
}

// AutoscaleProviderRequestAPI is the create/update body for a provider config.
type AutoscaleProviderRequestAPI struct {
	Name         string          `json:"name,omitempty"`
	ProviderType string          `json:"provider_type,omitempty"`
	Config       json.RawMessage `json:"config,omitempty"`
	Enabled      *bool           `json:"enabled,omitempty"`
}

// AutoscaleStatusAPI fetches the autoscaling status.
func (c *APIClient) AutoscaleStatusAPI() (*AutoscaleStatusResponseAPI, error) {
	data, err := c.GET("/api/autoscale/status")
	if err != nil {
		return nil, err
	}
	var resp AutoscaleStatusResponseAPI
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &resp, nil
}

// SetInstanceProtectedAPI toggles instance protection.
func (c *APIClient) SetInstanceProtectedAPI(instanceID string, protected bool) error {
	action := "unprotect"
	if protected {
		action = "protect"
	}
	_, err := c.POST("/api/autoscale/instances/"+instanceID+"/"+action, nil)
	return err
}

// ListAutoscaleProvidersAPI lists provider configs.
func (c *APIClient) ListAutoscaleProvidersAPI() ([]AutoscaleProviderAPI, error) {
	data, err := c.GET("/api/autoscale/providers")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Providers []AutoscaleProviderAPI `json:"providers"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return resp.Providers, nil
}

// GetAutoscaleProviderAPI fetches a provider config.
func (c *APIClient) GetAutoscaleProviderAPI(name string) (*AutoscaleProviderAPI, error) {
	data, err := c.GET("/api/autoscale/providers/" + name)
	if err != nil {
		return nil, err
	}
	var p AutoscaleProviderAPI
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &p, nil
}

// CreateAutoscaleProviderAPI creates a provider config.
func (c *APIClient) CreateAutoscaleProviderAPI(req AutoscaleProviderRequestAPI) error {
	_, err := c.POST("/api/autoscale/providers", req)
	return err
}

// UpdateAutoscaleProviderAPI updates a provider config.
func (c *APIClient) UpdateAutoscaleProviderAPI(name string, req AutoscaleProviderRequestAPI) error {
	_, err := c.PUT("/api/autoscale/providers/"+name, req)
	return err
}

// DeleteAutoscaleProviderAPI deletes a provider config.
func (c *APIClient) DeleteAutoscaleProviderAPI(name string) error {
	_, err := c.DELETE("/api/autoscale/providers/" + name)
	return err
}
