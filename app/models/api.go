package models

import (
	"encoding/json"
	"time"
)

type APIErrorResponse struct {
	Errors []string `json:"errors,omitempty"`
}

// GenericTask is a generalized task structure with only queue and timeout as required fields.
// The payload is stored as raw JSON to allow any structure.
type GenericTask struct {
	Queue               string          `json:"queue"`
	Timeout             int64           `json:"timeout"`
	Data                json.RawMessage `json:"data" swaggertype:"object"`
	DependsOn           []string        `json:"depends_on,omitempty"`
	OnParentFailure     string          `json:"on_parent_failure,omitempty"`
	InjectParentResults bool            `json:"inject_parent_results,omitempty"`
}
// Request contains the model input parameters for image/video generation.
// Fields are generic: send any fields you have, unused ones are ignored.
type Request struct {
	// ID is the request identifier
	ID string `json:"id"`
	// URL is an alternative webhook URL field
	URL string `json:"webhook_url"`
	// Prompt is the text prompt for generation - REQUIRED
	Prompt string `json:"prompt"`
	// NegativePrompt specifies what to avoid in generation
	NegativePrompt string `json:"negative_prompt,omitempty"`
	// Width of the output image in pixels
	Width int `json:"width,omitempty"`
	// Height of the output image in pixels
	Height int `json:"height,omitempty"`
	// NumSteps is the number of diffusion steps
	NumSteps int `json:"num_steps,omitempty"`
	// ClipSkip is the number of CLIP layers to skip
	ClipSkip int `json:"clip_skip,omitempty"`
	// GuidanceScale controls how closely to follow the prompt
	GuidanceScale float64 `json:"guidance_scale,omitempty"`
	// Seed for reproducible generation
	Seed int `json:"seed,omitempty"`
	// Quality level: "hd", "standard"
	Quality string `json:"quality,omitempty"`
	// BatchNbr is the number of images to generate
	BatchNbr int `json:"batch_nbr,omitempty"`
	// NumFrames is the number of frames for video generation
	NumFrames int `json:"num_frames,omitempty"`
	// FPS is the frames per second for video output
	FPS int `json:"fps,omitempty"`
	// DecodeTimesteps for video decode process
	DecodeTimesteps float64 `json:"decode_timesteps,omitempty"`
	// DecodeNoiseScale for video decode process
	DecodeNoiseScale float64 `json:"decode_noise_scale,omitempty"`
	// ImgURL is the input image URL for img2img generation
	ImgURL string `json:"img_url,omitempty"`
	// Scheduler is the sampling scheduler to use
	Scheduler string `json:"scheduler,omitempty"`
	// ModelName is the model to use
	ModelName string `json:"model_name,omitempty"`
	// OutputCompression controls image compression 0-100
	OutputCompression int `json:"output_compression,omitempty"`
	// OutputFormat specifies output format: "png", "jpeg"
	OutputFormat string `json:"output_format,omitempty"`
	// AspectRatio for image generation
	AspectRatio string `json:"aspect_ratio,omitempty"`
	// ImageSize for output resolution
	ImageSize string `json:"image_size,omitempty"`
	// Resolution for video generation
	Resolution string `json:"resolution,omitempty"`
	// DurationSeconds for video length
	DurationSeconds int `json:"duration_seconds,omitempty"`
	// PersonGeneration controls people generation
	PersonGeneration string `json:"person_generation,omitempty"`
}

type GenericResponsePost struct {
	Info                AddTaskInfoDoc   `json:"info"`
	Data                json.RawMessage  `json:"data" swaggertype:"object"`
	DependsOn           []DependencyInfo `json:"depends_on,omitempty"`
	OnParentFailure     string           `json:"on_parent_failure,omitempty"`
	InjectParentResults bool             `json:"inject_parent_results,omitempty"`
}
type ResponseGet struct {
	Info GetTaskInfoDoc
}

type AddTaskInfoDoc struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Payload       []byte `json:"payload"`
	State         string `json:"state"`
	Queue         string `json:"queue"`
	MaxRetry      int    `json:"max_retry"`
	Retried       int    `json:"retried"`
	LastErr       string
	LastFailedAt  time.Time
	Deadline      time.Time
	Group         string
	NextProcessAt time.Time
	IsOrphaned    bool
	CompletedAt   time.Time
	Result        []byte
}
type GetTaskInfoDoc struct {
	ID                  string           `json:"id"`
	Type                string           `json:"type"`
	Payload             string           `json:"payload"`
	State               string           `json:"state"`
	Queue               string           `json:"queue"`
	MaxRetry            int              `json:"max_retry"`
	Retried             int              `json:"retried"`
	LastErr             string           `json:"last_err,omitempty"`
	LastFailedAt        time.Time        `json:"last_failed_at,omitempty"`
	Deadline            time.Time        `json:"deadline,omitempty"`
	Group               string           `json:"group,omitempty"`
	NextProcessAt       time.Time        `json:"next_process_at,omitempty"`
	IsOrphaned          bool             `json:"is_orphaned,omitempty"`
	CompletedAt         time.Time        `json:"completed_at,omitempty"`
	Result              string           `json:"result,omitempty"`
	DependsOn           []DependencyInfo `json:"depends_on,omitempty"`
	OnParentFailure     string           `json:"on_parent_failure,omitempty"`
	InjectParentResults bool             `json:"inject_parent_results,omitempty"`
}

// TaskDependency represents a row in the task_dependencies table.
type TaskDependency struct {
	ID          int    `db:"id" json:"id"`
	ChildID     string `db:"child_id" json:"child_id"`
	ParentID    string `db:"parent_id" json:"parent_id"`
	ParentState string `db:"parent_state" json:"parent_state"`
	CreatedAt   int64  `db:"created_at" json:"created_at"`
}

// WaitingTask represents a row in the waiting_tasks table.
type WaitingTask struct {
	ID                  int    `db:"id" json:"id"`
	TaskID              string `db:"task_id" json:"task_id"`
	Queue               string `db:"queue" json:"queue"`
	Payload             []byte `db:"payload" json:"payload"`
	OnParentFailure     string `db:"on_parent_failure" json:"on_parent_failure"`
	InjectParentResults bool   `db:"inject_parent_results" json:"inject_parent_results"`
	Timeout             int64  `db:"timeout" json:"timeout"`
	CreatedAt           int64  `db:"created_at" json:"created_at"`
}

// DependencyInfo describes one parent dependency in API responses.
type DependencyInfo struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// TypedPayload represents the validated input fields sent to workers.
// It is simply a map of field name → value (no wrapper field).
type TypedPayload map[string]interface{}
