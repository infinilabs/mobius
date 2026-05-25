import axios from 'axios';
import type { SettingsData, Conversation, ConversationSummary, FileRef, Prompt, VertexModel, Employee, EmployeeMemory, Skill, Task, TaskComment, Project, ProjectAsset } from './types';
import { log } from './logger';

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

export async function listConversations(): Promise<ConversationSummary[]> {
  log.debug('api', 'listConversations');
  const { data } = await axios.get('/api/conversations');
  log.debug('api', 'conversations listed', { count: data.length });
  return data;
}

export async function createConversation(): Promise<Conversation> {
  log.info('api', 'creating conversation');
  const { data } = await axios.post('/api/conversations');
  log.info('api', 'conversation created', { id: data.id });
  return data;
}

export async function getConversation(id: string): Promise<Conversation> {
  log.debug('api', 'getConversation', { id });
  const { data } = await axios.get(`/api/conversations/${id}`);
  log.debug('api', 'conversation loaded', { id, messages: data.messages?.length ?? 0 });
  return data;
}

export async function renameConversation(id: string, title: string): Promise<void> {
  log.info('api', 'renaming conversation', { id, title });
  await axios.put(`/api/conversations/${id}`, { title });
}

export async function deleteConversation(id: string): Promise<void> {
  log.info('api', 'deleting conversation', { id });
  await axios.delete(`/api/conversations/${id}`);
}

export async function truncateConversation(id: string, keepCount: number): Promise<Conversation> {
  log.info('api', 'truncating conversation', { id, keepCount });
  const { data } = await axios.post(`/api/conversations/${id}/truncate`, { keep_count: keepCount });
  return data;
}

export async function uploadFile(file: File): Promise<FileRef> {
  log.info('api', 'uploading file', { name: file.name, size: file.size });
  const form = new FormData();
  form.append('file', file);
  const { data } = await axios.post('/api/upload', form);
  log.info('api', 'file uploaded', { id: data.id, name: data.name });
  return data;
}

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

// Skills
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

export async function listEmployeeSkills(employeeId: string): Promise<Skill[]> {
  const { data } = await axios.get(`/api/employees/${employeeId}/skills`);
  return data;
}

export async function assignSkillToEmployee(employeeId: string, skillId: string): Promise<void> {
  await axios.post(`/api/employees/${employeeId}/skills`, { skill_id: skillId });
}

export async function unassignSkillFromEmployee(employeeId: string, skillId: string): Promise<void> {
  await axios.delete(`/api/employees/${employeeId}/skills/${skillId}`);
}

export async function resetEmployeeSkills(employeeId: string): Promise<void> {
  await axios.post(`/api/employees/${employeeId}/skills/reset`);
}

// Employee Memories
export async function listEmployeeMemories(employeeId: string, query?: string): Promise<EmployeeMemory[]> {
  const params = query ? { q: query } : {};
  const { data } = await axios.get(`/api/employees/${employeeId}/memories`, { params });
  return data;
}

export async function addEmployeeMemory(employeeId: string, memoryText: string, conversationId?: string): Promise<void> {
  await axios.post(`/api/employees/${employeeId}/memories`, { memory_text: memoryText, conversation_id: conversationId || '' });
}

export async function deleteEmployeeMemory(employeeId: string, memoryId: string): Promise<void> {
  await axios.delete(`/api/employees/${employeeId}/memories/${memoryId}`);
}

// Employees
export async function listEmployees(): Promise<Employee[]> {
  const { data } = await axios.get('/api/employees');
  return data;
}

export async function getEmployee(id: string): Promise<Employee> {
  const { data } = await axios.get(`/api/employees/${id}`);
  return data;
}

export async function createEmployee(emp: Partial<Employee>): Promise<Employee> {
  const { data } = await axios.post('/api/employees', emp);
  return data;
}

export async function updateEmployee(id: string, emp: Partial<Employee>): Promise<Employee> {
  const { data } = await axios.put(`/api/employees/${id}`, emp);
  return data;
}

export async function deleteEmployee(id: string): Promise<void> {
  await axios.delete(`/api/employees/${id}`);
}

export async function setEmployeeManager(id: string, managerId: string): Promise<void> {
  await axios.put(`/api/employees/${id}/manager`, { manager_id: managerId });
}

// Delegation
export async function hireEmployee(hire: {
  hiring_manager_id: string;
  name: string;
  title: string;
  backstory: string;
  role?: string;
  primary_llm?: string;
  skills?: { skill: string; description: string }[];
}): Promise<Employee> {
  const { data } = await axios.post('/api/employees/hire', hire);
  return data;
}

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

// Tasks
export async function listTasks(filters?: { status?: string; assignee_id?: string; project_id?: string }): Promise<Task[]> {
  const params = new URLSearchParams();
  if (filters?.status) params.set('status', filters.status);
  if (filters?.assignee_id) params.set('assignee_id', filters.assignee_id);
  if (filters?.project_id) params.set('project_id', filters.project_id);
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

// Project Memory
export async function getProjectMemory(projectId: string): Promise<{ content: string }> {
  const { data } = await axios.get(`/api/projects/${projectId}/memory`);
  return data;
}

export async function updateProjectMemory(projectId: string, content: string): Promise<void> {
  await axios.put(`/api/projects/${projectId}/memory`, { content });
}

export async function browseDirectories(path?: string): Promise<{ current: string; parent: string; dirs: { name: string; path: string }[] }> {
  const params = path ? `?path=${encodeURIComponent(path)}` : '';
  const { data } = await axios.get(`/api/browse-directories${params}`);
  return data;
}

export async function sendChatMessage(
  conversationId: string,
  message: string,
  onChunk: (text: string) => void,
  onDone: () => void,
  onError: (error: string) => void,
  files?: FileRef[],
  agentId?: string,
  modelId?: string,
): Promise<void> {
  log.info('chat', 'sending message', { conversationId, length: message.length, files: files?.length ?? 0, agentId, modelId });

  let response: Response;
  try {
    response = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        conversation_id: conversationId,
        message,
        files: files?.length ? files : undefined,
        agent_id: agentId || undefined,
        model_id: modelId || undefined,
      }),
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : 'Network error';
    log.error('chat', 'fetch failed', { error: msg });
    onError(msg);
    return;
  }

  if (!response.ok) {
    log.error('chat', 'HTTP error', { status: response.status });
    onError(`HTTP ${response.status}`);
    return;
  }

  const reader = response.body?.getReader();
  if (!reader) {
    log.error('chat', 'no response stream');
    onError('No response stream');
    return;
  }

  const decoder = new TextDecoder();
  let buffer = '';
  let chunkCount = 0;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() || '';

    for (const line of lines) {
      if (!line.startsWith('data: ')) continue;
      const json_str = line.slice(6).trim();
      if (!json_str) continue;

      try {
        const parsed = JSON.parse(json_str);
        if (parsed.done) {
          log.info('chat', 'stream complete', { conversationId, chunks: chunkCount });
          onDone();
          return;
        }
        if (parsed.error) {
          log.error('chat', 'stream error from server', { error: parsed.error });
          onError(parsed.error);
          return;
        }
        if (parsed.text) {
          chunkCount++;
          onChunk(parsed.text);
        }
      } catch {
        log.warn('chat', 'malformed SSE chunk', { raw: json_str.slice(0, 100) });
      }
    }
  }
  log.info('chat', 'stream ended', { conversationId, chunks: chunkCount });
  onDone();
}
