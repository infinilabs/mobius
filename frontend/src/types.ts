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
  default?: boolean;
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
  api_key: string;
  bigquery: BigQueryConfig;
  gcs: GCSConfig;
  vertex_ai: VertexAIConfig;
}

export interface UploadConfig {
  max_file_size_mb: number;
}

export interface SettingsData {
  postgres: PostgresConfig;
  elasticsearch: ElasticsearchConfig;
  google_cloud: GoogleCloudConfig;
  upload: UploadConfig;
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
  project_id?: string | null;
  created_at: number;
  updated_at: number;
}

export interface ConversationSummary {
  id: string;
  title: string;
  project_id?: string | null;
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

export interface Skill {
  id: string;
  name: string;
  description: string;
  category: string;
  content: string;
  tags: string[];
  version: string;
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

export interface Task {
  id: string;
  title: string;
  body: string;
  status: 'todo' | 'ready' | 'in_progress' | 'needs_review' | 'done' | 'blocked' | 'scheduled';
  priority: 'low' | 'medium' | 'high' | 'urgent';
  assignee: EmployeeBrief | null;
  creator: EmployeeBrief | null;
  result: string;
  failure_count: number;
  dependencies: string[];
  is_scheduled: boolean;
  cron_expr?: string;
  next_run_at?: string;
  repeat_times?: number | null;
  parent_task_id?: string | null;
  project_id?: string | null;
  project_name?: string;
  created_at: string;
  updated_at: string;
  completed_at?: string;
}

export interface TaskComment {
  id: string;
  task_id: string;
  author: EmployeeBrief | null;
  content: string;
  created_at: string;
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
  tags: string[];
  manager_id?: string | null;
  reports: EmployeeBrief[];
  created_at: string;
  updated_at: string;
}

export interface EmployeeMemory {
  id: string;
  employee_id: string;
  conversation_id: string;
  memory_text: string;
  created_at: string;
  updated_at: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  owner: EmployeeBrief | null;
  status: 'active' | 'paused';
  source_path?: string;
  tags: string[];
  task_count: number;
  asset_count: number;
  created_at: string;
  updated_at: string;
}

export interface ProjectAsset {
  id: string;
  project_id: string;
  filename: string;
  relative_path: string;
  mime_type: string;
  size_bytes: number;
  content?: string;
  content_summary?: string;
  content_truncated: boolean;
  content_type: 'text' | 'code' | 'document' | 'pdf' | 'image' | 'video' | 'audio' | 'binary';
  gcs_uri?: string;
  gcs_status: 'pending' | 'synced' | 'failed';
  tags: string[];
  created_by?: EmployeeBrief | null;
  task_id?: string;
  created_at: string;
  updated_at: string;
}
