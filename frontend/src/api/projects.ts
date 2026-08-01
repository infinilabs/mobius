import axios from 'axios';
import type { Project, ProjectAsset } from '../types';

// Projects
export async function listProjects(status?: string): Promise<Project[]> {
  const params = status ? `?status=${encodeURIComponent(status)}` : '';
  const { data } = await axios.get(`/api/projects${params}`);
  return data;
}

export async function createProject(project: { name: string; description: string; owner_id?: string; tags?: string[]; source_path?: string }): Promise<Project> {
  const { data } = await axios.post('/api/projects', project);
  return data;
}

export async function getProject(id: string): Promise<Project> {
  const { data } = await axios.get(`/api/projects/${id}`);
  return data;
}

export async function updateProject(id: string, fields: { name?: string; description?: string; status?: string }): Promise<Project> {
  const { data } = await axios.put(`/api/projects/${id}`, fields);
  return data;
}

export async function deleteProject(id: string, mode: 'archive' | 'delete' = 'archive'): Promise<{ archive_path: string }> {
  const { data } = await axios.delete(`/api/projects/${id}?mode=${mode}`);
  return data;
}

// Project Assets
export async function listProjectAssets(projectId: string, query?: string, type?: string): Promise<ProjectAsset[]> {
  const params = new URLSearchParams();
  if (query) params.set('q', query);
  if (type) params.set('type', type);
  const qs = params.toString();
  const { data } = await axios.get(`/api/projects/${projectId}/assets${qs ? `?${qs}` : ''}`);
  return data;
}

export async function uploadProjectAsset(projectId: string, file: File, path?: string): Promise<ProjectAsset> {
  const form = new FormData();
  form.append('file', file);
  if (path) form.append('path', path);
  const { data } = await axios.post(`/api/projects/${projectId}/assets`, form);
  return data;
}

export async function deleteProjectAsset(projectId: string, assetId: string): Promise<void> {
  await axios.delete(`/api/projects/${projectId}/assets/${assetId}`);
}

export async function reindexProjectAssets(projectId: string): Promise<{ indexed: number }> {
  const { data } = await axios.post(`/api/projects/${projectId}/assets/reindex`);
  return data;
}

// assetContentUrl returns the URL that streams an asset's raw bytes, for use directly in
// <img src> / <iframe src> previews.
export function assetContentUrl(projectId: string, assetId: string): string {
  return `/api/projects/${projectId}/assets/${assetId}/content`;
}

export interface CreativeFilters {
  q?: string;
  type?: string;
  tag?: string;
  origin?: string;
  aspect_ratio?: string;
  status?: string;
  date_from?: string;
  date_to?: string;
}

// listCreatives fetches the curated, global creatives library across all projects,
// filtered by the supplied facets (content type, tag, origin, aspect ratio, status,
// published-date range).
export async function listCreatives(filters: CreativeFilters = {}): Promise<ProjectAsset[]> {
  const params = new URLSearchParams();
  Object.entries(filters).forEach(([k, v]) => { if (v) params.set(k, v); });
  const qs = params.toString();
  const { data } = await axios.get(`/api/creatives${qs ? `?${qs}` : ''}`);
  return data;
}

// listCreativeTags returns the distinct tags across creatives for the quick-filter chips.
export async function listCreativeTags(): Promise<string[]> {
  const { data } = await axios.get('/api/creatives/tags');
  return data;
}

// addAssetToCreatives promotes a project asset into the Creatives library (persists to
// GCS + tags "creative"). Returns the updated asset.
export async function addAssetToCreatives(projectId: string, assetId: string): Promise<ProjectAsset> {
  const { data } = await axios.post(`/api/projects/${projectId}/assets/${assetId}/creative`);
  return data;
}

// uploadCreative uploads a file straight from the local computer into the Creatives
// library (stored under a reserved project, tagged "creative"). Returns the new asset.
export async function uploadCreative(file: File): Promise<ProjectAsset> {
  const form = new FormData();
  form.append('file', file);
  const { data } = await axios.post('/api/creatives/upload', form);
  return data;
}

// updateCreativeMeta updates asset/creative metadata (title, description, status, tags)
// without touching file content.
export async function updateCreativeMeta(
  projectId: string,
  assetId: string,
  body: { title?: string; description?: string; status?: string; tags?: string[] },
): Promise<ProjectAsset> {
  const { data } = await axios.patch(`/api/projects/${projectId}/assets/${assetId}/meta`, body);
  return data;
}

// updateProjectAssetContent overwrites a text/code asset's file content and reindexes.
export async function updateProjectAssetContent(projectId: string, assetId: string, content: string): Promise<ProjectAsset> {
  const { data } = await axios.put(`/api/projects/${projectId}/assets/${assetId}`, { content });
  return data;
}

// Project Memory
export async function getProjectMemory(projectId: string): Promise<{ content: string }> {
  const { data } = await axios.get(`/api/projects/${projectId}/memory`);
  return data;
}

export async function updateProjectMemory(projectId: string, content: string): Promise<void> {
  await axios.put(`/api/projects/${projectId}/memory`, { content });
}
