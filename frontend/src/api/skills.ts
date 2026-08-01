import axios from 'axios';
import type { Skill } from '../types';

export async function listSkills(query?: string): Promise<Skill[]> {
  const params = query ? `?q=${encodeURIComponent(query)}` : '';
  const { data } = await axios.get(`/api/skills${params}`);
  return data;
}

export async function getSkill(id: string): Promise<Skill> {
  const { data } = await axios.get(`/api/skills/${id}`);
  return data;
}

export async function createSkill(skill: { name: string; description: string; category: string; content: string; tags: string[]; version?: string }): Promise<Skill> {
  const { data } = await axios.post('/api/skills', skill);
  return data;
}

export async function updateSkill(id: string, skill: { name?: string; description?: string; category?: string; content?: string; tags?: string[]; version?: string }): Promise<Skill> {
  const { data } = await axios.put(`/api/skills/${id}`, skill);
  return data;
}

export async function deleteSkill(id: string): Promise<void> {
  await axios.delete(`/api/skills/${id}`);
}

export interface SyncResult {
  sources: Array<{ name: string; added: number; updated: number; error: string }>;
  disk_sync: { added: number; updated: number };
  synced_at: string;
}

export async function syncSkills(): Promise<SyncResult> {
  const { data } = await axios.post('/api/skills/sync');
  return data;
}
