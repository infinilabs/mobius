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

export interface VertexModel {
  id: string;
  name: string;
  model_id: string;
  location: string;
  type: 'llm' | 'image' | 'video';
}

export interface VertexAIConfig {
  llm_model_id?: string;
  llm_location?: string;
  img_model_id?: string;
  img_location?: string;
  video_model_id?: string;
  video_location?: string;
  models?: VertexModel[];
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

export interface EmployeeModel {
  model_id: string;
  purpose: string;
}

export interface EmployeeSkill {
  skill: string;
  description: string;
}

export interface EmployeeBrief {
  id: string;
  name: string;
  title: string;
  role: string;
}

export interface Employee {
  id: string;
  name: string;
  title: string;
  role: string;
  backstory: string;
  avatar_url: string;
  models: EmployeeModel[];
  skills: EmployeeSkill[];
  manager_id?: string | null;
  reports: EmployeeBrief[];
  created_at: string;
  updated_at: string;
}
