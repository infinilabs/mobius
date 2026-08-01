import axios from 'axios';
import type { SettingsData, VertexModel, SearchResult } from '../types';
import { log } from '../logger';

export async function fetchConfig(): Promise<{ project_id: string }> {
  log.debug('api', 'fetchConfig');
  const { data } = await axios.get('/api/config');
  log.info('api', 'config loaded', { project_id: data.project_id });
  return data;
}

export interface ServiceStatus {
  status: string;
  error?: string;
}

export interface HealthData {
  status: string;
  services: Record<string, ServiceStatus>;
}

export async function fetchHealth(): Promise<HealthData> {
  const { data } = await axios.get('/api/health');
  return data;
}

export async function fetchSettings(): Promise<SettingsData> {
  log.debug('api', 'fetchSettings');
  const { data } = await axios.get('/api/settings');
  log.info('api', 'settings loaded', { model: data.google_cloud?.vertex_ai?.llm_model_id });
  return data;
}

export async function updateSettings(settings: SettingsData): Promise<SettingsData> {
  log.info('api', 'updating settings');
  const { data } = await axios.put('/api/settings', settings);
  log.info('api', 'settings saved');
  return data;
}

export async function listModels(): Promise<VertexModel[]> {
  const { data } = await axios.get('/api/models');
  return data;
}

export async function addModel(model: Omit<VertexModel, 'name'> & { name?: string }): Promise<VertexModel> {
  const { data } = await axios.post('/api/models', model);
  return data;
}

export async function removeModel(id: string): Promise<void> {
  await axios.delete(`/api/models/${id}`);
}

export async function browseDirectories(path?: string): Promise<{ current: string; parent: string; dirs: { name: string; path: string }[] }> {
  const params = path ? `?path=${encodeURIComponent(path)}` : '';
  const { data } = await axios.get(`/api/browse-directories${params}`);
  return data;
}

// Search
export async function searchEntities(type: string, query: string, limit = 10): Promise<SearchResult[]> {
  const params = new URLSearchParams({ type, limit: String(limit) });
  if (query) params.set('q', query);
  const { data } = await axios.get(`/api/search?${params}`);
  return data;
}
