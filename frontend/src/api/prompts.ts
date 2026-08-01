import axios from 'axios';
import type { Prompt } from '../types';

export async function listPrompts(query?: string): Promise<Prompt[]> {
  const params = query ? `?q=${encodeURIComponent(query)}` : '';
  const { data } = await axios.get(`/api/prompts${params}`);
  return data;
}

export async function createPrompt(prompt: { title: string; content: string; tags: string[] }): Promise<Prompt> {
  const { data } = await axios.post('/api/prompts', prompt);
  return data;
}

export async function updatePrompt(id: string, prompt: { title?: string; content?: string; tags?: string[] }): Promise<Prompt> {
  const { data } = await axios.put(`/api/prompts/${id}`, prompt);
  return data;
}

export async function deletePrompt(id: string): Promise<void> {
  await axios.delete(`/api/prompts/${id}`);
}
