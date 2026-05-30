package main

import (
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

type ProjectConfig struct {
	ProjectsDir      string   `yaml:"projects_dir" json:"projects_dir"`
	ConversationsDir string   `yaml:"conversations_dir" json:"conversations_dir"`
	TemplateDirs     []string `yaml:"template_dirs" json:"template_dirs"`

	MemoryMaxSize      int     `yaml:"memory_max_size" json:"memory_max_size"`
	MemoryCompactRatio float64 `yaml:"memory_compact_ratio" json:"memory_compact_ratio"`
	MemoryCompactKeep  int     `yaml:"memory_compact_keep" json:"memory_compact_keep"`
	MemoryInjectLimit  int     `yaml:"memory_inject_limit" json:"memory_inject_limit"`
	MemoryDedupPrefix  int     `yaml:"memory_dedup_prefix" json:"memory_dedup_prefix"`

	ContentMaxIndex   int `yaml:"content_max_index" json:"content_max_index"`
	ContentSummaryMax int `yaml:"content_summary_max" json:"content_summary_max"`

	GCSMaxRetries  int `yaml:"gcs_max_retries" json:"gcs_max_retries"`
	GCSBaseBackoff int `yaml:"gcs_base_backoff_ms" json:"gcs_base_backoff_ms"`

	ESShards   int `yaml:"es_shards" json:"es_shards"`
	ESReplicas int `yaml:"es_replicas" json:"es_replicas"`
}

func (pc *ProjectConfig) applyDefaults(workdir string) {
	if pc.ProjectsDir == "" {
		pc.ProjectsDir = filepath.Join(workdir, "projects")
	}
	pc.ProjectsDir, _ = filepath.Abs(pc.ProjectsDir)
	if pc.ConversationsDir == "" {
		pc.ConversationsDir = filepath.Join(workdir, "conversations")
	}
	pc.ConversationsDir, _ = filepath.Abs(pc.ConversationsDir)
	if len(pc.TemplateDirs) == 0 {
		pc.TemplateDirs = []string{"reports", "code", "media", "data", "docs"}
	}
	if pc.MemoryMaxSize == 0 {
		pc.MemoryMaxSize = 40 * 1024
	}
	if pc.MemoryCompactRatio == 0 {
		pc.MemoryCompactRatio = 0.5
	}
	if pc.MemoryCompactKeep == 0 {
		pc.MemoryCompactKeep = 20
	}
	if pc.MemoryInjectLimit == 0 {
		pc.MemoryInjectLimit = 8000
	}
	if pc.MemoryDedupPrefix == 0 {
		pc.MemoryDedupPrefix = 60
	}
	if pc.ContentMaxIndex == 0 {
		pc.ContentMaxIndex = 100 * 1024
	}
	if pc.ContentSummaryMax == 0 {
		pc.ContentSummaryMax = 2000
	}
	if pc.GCSMaxRetries == 0 {
		pc.GCSMaxRetries = 3
	}
	if pc.GCSBaseBackoff == 0 {
		pc.GCSBaseBackoff = 1000
	}
	if pc.ESShards == 0 {
		pc.ESShards = 1
	}
}

type PostgresConfig struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`
	User     string `yaml:"user" json:"user"`
	Password string `yaml:"password" json:"password"`
	DBName   string `yaml:"dbname" json:"dbname"`
}

type ElasticsearchConfig struct {
	URL    string      `yaml:"url" json:"url"`
	Events EventConfig `yaml:"events,omitempty" json:"events,omitempty"`
}

type VertexModel struct {
	ID       string `yaml:"id" json:"id"`
	Name     string `yaml:"name" json:"name"`
	ModelID  string `yaml:"model_id" json:"model_id"`
	Location string `yaml:"location" json:"location"`
	Type     string `yaml:"type" json:"type"`
	Default  bool   `yaml:"default,omitempty" json:"default,omitempty"`
}

type VertexAIConfig struct {
	ProjectID     string `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	LLMModelID    string `yaml:"llm_model_id,omitempty" json:"llm_model_id,omitempty"`
	LLMLocation   string `yaml:"llm_location,omitempty" json:"llm_location,omitempty"`
	ImgModelID    string `yaml:"img_model_id,omitempty" json:"img_model_id,omitempty"`
	ImgLocation   string `yaml:"img_location,omitempty" json:"img_location,omitempty"`
	VideoModelID  string `yaml:"video_model_id,omitempty" json:"video_model_id,omitempty"`
	VideoLocation string `yaml:"video_location,omitempty" json:"video_location,omitempty"`
	Models        []VertexModel `yaml:"models,omitempty" json:"models,omitempty"`
}

func (v *VertexAIConfig) GetModels() []VertexModel {
	if len(v.Models) > 0 {
		return v.Models
	}
	var models []VertexModel
	if v.LLMModelID != "" {
		models = append(models, VertexModel{
			ID: v.LLMModelID, Name: v.LLMModelID,
			ModelID: v.LLMModelID, Location: v.LLMLocation, Type: "llm",
		})
	}
	if v.ImgModelID != "" {
		models = append(models, VertexModel{
			ID: v.ImgModelID, Name: v.ImgModelID,
			ModelID: v.ImgModelID, Location: v.ImgLocation, Type: "image",
		})
	}
	if v.VideoModelID != "" {
		models = append(models, VertexModel{
			ID: v.VideoModelID, Name: v.VideoModelID,
			ModelID: v.VideoModelID, Location: v.VideoLocation, Type: "video",
		})
	}
	return models
}

func (v *VertexAIConfig) DefaultModel(modelType string) (modelID, location string) {
	models := v.GetModels()
	var first VertexModel
	var found bool
	for _, m := range models {
		if m.Type != modelType {
			continue
		}
		if m.Default {
			return m.ModelID, m.Location
		}
		if !found {
			first = m
			found = true
		}
	}
	if found {
		return first.ModelID, first.Location
	}
	return "", ""
}

func (v *VertexAIConfig) DefaultLLM() (modelID, location string) {
	mid, loc := v.DefaultModel("llm")
	if mid != "" {
		return mid, loc
	}
	return v.LLMModelID, v.LLMLocation
}

type EventConfig struct {
	BufferSize     int    `yaml:"buffer_size" json:"buffer_size"`
	BatchSize      int    `yaml:"batch_size" json:"batch_size"`
	FlushIntervalS int    `yaml:"flush_interval_s" json:"flush_interval_s"`
	RetentionDays  int    `yaml:"retention_days" json:"retention_days"`
	ArchiveCron    string `yaml:"archive_cron" json:"archive_cron"`
}

func (ec *EventConfig) applyDefaults() {
	if ec.BufferSize == 0 {
		ec.BufferSize = 5000
	}
	if ec.BatchSize == 0 {
		ec.BatchSize = 50
	}
	if ec.FlushIntervalS == 0 {
		ec.FlushIntervalS = 5
	}
	if ec.RetentionDays == 0 {
		ec.RetentionDays = 90
	}
	if ec.ArchiveCron == "" {
		ec.ArchiveCron = "0 3 * * *"
	}
}

type BigQueryConfig struct {
	ProjectID  string `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	Dataset    string `yaml:"dataset" json:"dataset"`
	EventTable string `yaml:"event_table" json:"event_table"`
	TokenTable string `yaml:"token_table" json:"token_table"`
}

type GCSConfig struct {
	ProjectID              string `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	Bucket                 string `yaml:"bucket" json:"bucket"`
	Location               string `yaml:"location" json:"location"`
	PublicAccessPrevention bool   `yaml:"public_access_prevention" json:"public_access_prevention"`
	EventArchivePrefix     string `yaml:"event_archive_prefix" json:"event_archive_prefix"`
}

type GoogleCloudConfig struct {
	ProjectID       string         `yaml:"project_id" json:"project_id"`
	CredentialsPath string         `yaml:"credentials_path" json:"credentials_path"`
	APIKey          string         `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	BigQuery        BigQueryConfig `yaml:"bigquery" json:"bigquery"`
	GCS             GCSConfig      `yaml:"gcs" json:"gcs"`
	VertexAI        VertexAIConfig `yaml:"vertex_ai" json:"vertex_ai"`
}

type Config struct {
	mu sync.RWMutex `yaml:"-"`

	Server struct {
		Port          int    `yaml:"port" json:"port"`
		Mode          string `yaml:"mode" json:"mode"`
		LogMaxSizeMB  int    `yaml:"log_max_size_mb" json:"log_max_size_mb"`
		LogMaxBackups int    `yaml:"log_max_backups" json:"log_max_backups"`
		LogMaxAgeDays int    `yaml:"log_max_age_days" json:"log_max_age_days"`
	} `yaml:"server" json:"server"`

	Postgres      PostgresConfig      `yaml:"postgres" json:"postgres"`
	Elasticsearch ElasticsearchConfig `yaml:"elasticsearch" json:"elasticsearch"`
	GoogleCloud   GoogleCloudConfig   `yaml:"google_cloud" json:"google_cloud"`
	Projects      ProjectConfig       `yaml:"projects,omitempty" json:"projects,omitempty"`
	Sandbox       SandboxConfig       `yaml:"sandbox,omitempty" json:"sandbox,omitempty"`

	Upload struct {
		MaxFileSizeMB int `yaml:"max_file_size_mb,omitempty" json:"max_file_size_mb,omitempty"`
	} `yaml:"chat_upload,omitempty" json:"chat_upload,omitempty"`

	SkillSync struct {
		HermesPath string `yaml:"hermes_path,omitempty" json:"hermes_path,omitempty"`
		Repos      []struct {
			Name     string   `yaml:"name" json:"name"`
			Path     string   `yaml:"path" json:"path"`
			Category string   `yaml:"category" json:"category"`
			Dirs     []string `yaml:"dirs" json:"dirs"`
		} `yaml:"repos,omitempty" json:"repos,omitempty"`
	} `yaml:"skill_sync,omitempty" json:"skill_sync,omitempty"`
}

type UploadConfig struct {
	MaxFileSizeMB int `json:"max_file_size_mb"`
}

type SettingsData struct {
	Postgres      PostgresConfig      `json:"postgres"`
	Elasticsearch ElasticsearchConfig `json:"elasticsearch"`
	GoogleCloud   GoogleCloudConfig   `json:"google_cloud"`
	Upload        UploadConfig        `json:"chat_upload"`
}

func (c *Config) MaxUploadBytes() int64 {
	mb := c.Upload.MaxFileSizeMB
	if mb <= 0 {
		mb = 20
	}
	return int64(mb) << 20
}

func (c *Config) GetSettings() SettingsData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	mb := c.Upload.MaxFileSizeMB
	if mb <= 0 {
		mb = 20
	}
	return SettingsData{
		Postgres:      c.Postgres,
		Elasticsearch: c.Elasticsearch,
		GoogleCloud:   c.GoogleCloud,
		Upload:        UploadConfig{MaxFileSizeMB: mb},
	}
}

func (c *Config) ApplySettings(s SettingsData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Postgres = s.Postgres
	c.Elasticsearch = s.Elasticsearch
	c.GoogleCloud = s.GoogleCloud
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	workdir, _ := os.Getwd()
	cfg.Projects.applyDefaults(workdir)
	cfg.Elasticsearch.Events.applyDefaults()
	cfg.Sandbox.applyDefaults()
	return &cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
