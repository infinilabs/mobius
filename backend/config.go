package main

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type PostgresConfig struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`
	User     string `yaml:"user" json:"user"`
	Password string `yaml:"password" json:"password"`
	DBName   string `yaml:"dbname" json:"dbname"`
}

type ElasticsearchConfig struct {
	URL string `yaml:"url" json:"url"`
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

type BigQueryConfig struct {
	Dataset string `yaml:"dataset" json:"dataset"`
}

type GCSConfig struct {
	Bucket                 string `yaml:"bucket" json:"bucket"`
	Location               string `yaml:"location" json:"location"`
	PublicAccessPrevention bool   `yaml:"public_access_prevention" json:"public_access_prevention"`
}

type GoogleCloudConfig struct {
	ProjectID       string         `yaml:"project_id" json:"project_id"`
	CredentialsPath string         `yaml:"credentials_path" json:"credentials_path"`
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

	Upload struct {
		MaxFileSizeMB int `yaml:"max_file_size_mb,omitempty" json:"max_file_size_mb,omitempty"`
	} `yaml:"upload,omitempty" json:"upload,omitempty"`

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
	Upload        UploadConfig        `json:"upload"`
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
