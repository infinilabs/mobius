import axios from 'axios';
import type { Task, TaskComment, TaskInteraction } from '../types';

// Tasks
export async function listTasks(filters?: { status?: string; assignee_id?: string; project_id?: string; conversation_id?: string }): Promise<Task[]> {
  const params = new URLSearchParams();
  if (filters?.status) params.set('status', filters.status);
  if (filters?.assignee_id) params.set('assignee_id', filters.assignee_id);
  if (filters?.project_id) params.set('project_id', filters.project_id);
  if (filters?.conversation_id) params.set('conversation_id', filters.conversation_id);
  const qs = params.toString();
  const { data } = await axios.get(`/api/tasks${qs ? `?${qs}` : ''}`);
  return data;
}

export async function createTask(task: { title: string; body?: string; priority?: string; assignee_id?: string; creator_id?: string; dependencies?: string[]; is_scheduled?: boolean; cron_expr?: string; repeat_times?: number; project_id?: string }): Promise<Task> {
  const { data } = await axios.post('/api/tasks', task);
  return data;
}

export async function getTask(id: string): Promise<Task> {
  const { data } = await axios.get(`/api/tasks/${id}`);
  return data;
}

export async function updateTask(id: string, fields: { title?: string; body?: string; priority?: string; assignee_id?: string; result?: string }): Promise<Task> {
  const { data } = await axios.put(`/api/tasks/${id}`, fields);
  return data;
}

export async function deleteTask(id: string): Promise<void> {
  await axios.delete(`/api/tasks/${id}`);
}

export async function updateTaskStatus(id: string, status: string, actorId?: string, feedback?: string): Promise<Task> {
  const { data } = await axios.put(`/api/tasks/${id}/status`, { status, actor_id: actorId || undefined, feedback: feedback || undefined });
  return data;
}

export async function listTaskComments(id: string): Promise<TaskComment[]> {
  const { data } = await axios.get(`/api/tasks/${id}/comments`);
  return data;
}

export async function addTaskComment(id: string, authorId: string, content: string): Promise<TaskComment> {
  const { data } = await axios.post(`/api/tasks/${id}/comments`, { author_id: authorId, content });
  return data;
}

export async function listTaskRuns(taskId: string): Promise<Task[]> {
  const { data } = await axios.get(`/api/tasks/${taskId}/runs`);
  return data;
}

export async function updateTaskSchedule(taskId: string, schedule: {
  cron_expr?: string;
  repeat_times?: number | null;
  is_scheduled?: boolean;
}): Promise<Task> {
  const { data } = await axios.put(`/api/tasks/${taskId}/schedule`, schedule);
  return data;
}

// Delegation
export async function delegateTask(delegation: {
  creator_id: string;
  assignee_id: string;
  title: string;
  goal: string;
  context?: string;
  priority?: string;
  conversation_id?: string;
  dependencies?: string[];
}): Promise<Task> {
  const { data } = await axios.post('/api/tasks/delegate', delegation);
  return data;
}

// Interactions
export async function listInteractions(taskId: string): Promise<TaskInteraction[]> {
  const { data } = await axios.get(`/api/tasks/${taskId}/interactions`);
  return data;
}

export async function resolveInteraction(taskId: string, interactionId: string, resolvedBy: string, response: Record<string, unknown>): Promise<void> {
  await axios.put(`/api/tasks/${taskId}/interactions/${interactionId}`, { resolved_by: resolvedBy, response });
}
