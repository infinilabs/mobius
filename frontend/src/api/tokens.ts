import axios from 'axios';
import type { TokenSummary, TokenTimeseriesPoint, TokenBreakdownItem, TokenDetailRow } from '../types';

// Token Monitor
export interface TokenFilters {
  since?: string;
  until?: string;
  model_id?: string[];
  employee_id?: string[];
  project_id?: string[];
  task_id?: string[];
  source?: string[];
}

function tokenParams(filters: TokenFilters, extra?: Record<string, string>): string {
  const params = new URLSearchParams();
  if (filters.since) params.set('since', filters.since);
  if (filters.until) params.set('until', filters.until);
  filters.model_id?.forEach(v => params.append('model_id', v));
  filters.employee_id?.forEach(v => params.append('employee_id', v));
  filters.project_id?.forEach(v => params.append('project_id', v));
  filters.task_id?.forEach(v => params.append('task_id', v));
  filters.source?.forEach(v => params.append('source', v));
  if (extra) Object.entries(extra).forEach(([k, v]) => params.set(k, v));
  return params.toString();
}

export async function fetchTokenSummary(filters: TokenFilters): Promise<{ current: TokenSummary; previous: TokenSummary | null }> {
  const { data } = await axios.get(`/api/tokens/summary?${tokenParams(filters)}`);
  return data;
}

export async function fetchTokenTimeseries(filters: TokenFilters, interval = 'DAY'): Promise<TokenTimeseriesPoint[]> {
  const { data } = await axios.get(`/api/tokens/timeseries?${tokenParams(filters, { interval })}`);
  return data;
}

export async function fetchTokenBreakdown(filters: TokenFilters, groupBy = 'model_id'): Promise<TokenBreakdownItem[]> {
  const { data } = await axios.get(`/api/tokens/breakdown?${tokenParams(filters, { group_by: groupBy })}`);
  return data;
}

export async function fetchTokenDetails(filters: TokenFilters, limit = 50, offset = 0, sort = 'timestamp', order = 'desc'): Promise<TokenDetailRow[]> {
  const { data } = await axios.get(`/api/tokens/details?${tokenParams(filters, { limit: String(limit), offset: String(offset), sort, order })}`);
  return data;
}
