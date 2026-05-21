export interface AppConfig {
  project_id: string;
}

export interface PostgresConfig {
  host: string;
  port: number;
  user: string;
  password: string;
  dbname: string;
}

export interface ElasticsearchConfig {
  url: string;
}

export interface VertexAIConfig {
  llm_model_id: string;
  llm_location: string;
  img_model_id: string;
  img_location: string;
  video_model_id: string;
  video_location: string;
}

export interface BigQueryConfig {
  dataset: string;
}

export interface GCSConfig {
  bucket: string;
  location: string;
  public_access_prevention: boolean;
}

export interface GoogleCloudConfig {
  project_id: string;
  credentials_path: string;
  bigquery: BigQueryConfig;
  gcs: GCSConfig;
  vertex_ai: VertexAIConfig;
}

export interface SettingsData {
  postgres: PostgresConfig;
  elasticsearch: ElasticsearchConfig;
  google_cloud: GoogleCloudConfig;
}

export interface FileRef {
  id: string;
  name: string;
  gcs_uri: string;
  mime_type: string;
  size: number;
}

export interface ChatMessage {
  role: 'user' | 'model';
  content: string;
  timestamp: number;
  files?: FileRef[];
}

export interface Conversation {
  id: string;
  title: string;
  messages: ChatMessage[];
  created_at: number;
  updated_at: number;
}

export interface ConversationSummary {
  id: string;
  title: string;
  updated_at: number;
}

export interface Prompt {
  id: string;
  title: string;
  content: string;
  tags: string[];
  created_at: number;
  updated_at: number;
}
