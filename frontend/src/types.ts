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

export interface BigQueryConfig {
  project_id: string;
  dataset: string;
  credentials_path: string;
  model_id: string;
  location: string;
}

export interface SettingsData {
  postgres: PostgresConfig;
  elasticsearch: ElasticsearchConfig;
  bigquery: BigQueryConfig;
}

export interface FileRef {
  id: string;
  name: string;
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
