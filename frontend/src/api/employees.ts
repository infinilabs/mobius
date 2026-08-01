import axios from 'axios';
import type { Employee, EmployeeMemory, Skill } from '../types';

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

// Employee skill assignments
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
