import { useState, useEffect } from 'react';
import {
  Settings, Database, Search, Cloud, Link2, Upload,
  Save, Check, AlertCircle, Loader2, Plus, Trash2,
} from 'lucide-react';
import { fetchSettings, updateSettings, fetchHealth, listModels, addModel, removeModel } from '../api';
import type { SettingsData, VertexModel } from '../types';
import type { ServiceStatus } from '../api';

const AD_PLATFORMS = [
  { name: 'Google Ads', desc: 'Search, Display, YouTube campaigns' },
  { name: 'Meta Ads', desc: 'Facebook & Instagram campaigns' },
  { name: 'TikTok Ads', desc: 'TikTok campaign management' },
];

type SaveStatus = 'idle' | 'saving' | 'saved' | 'error';

export default function SettingsPage() {
  const [settings, setSettings] = useState<SettingsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [saveStatus, setSaveStatus] = useState<SaveStatus>('idle');
  const [errorMsg, setErrorMsg] = useState('');
  const [health, setHealth] = useState<Record<string, ServiceStatus>>({});

  useEffect(() => {
    fetchSettings()
      .then(setSettings)
      .catch(() => setSettings({
        postgres: { host: 'localhost', port: 5432, user: 'mobius', password: 'mobius', dbname: 'mobius' },
        elasticsearch: { url: 'http://localhost:9200' },
        google_cloud: {
          project_id: '', credentials_path: '',
          bigquery: { dataset: 'mobius' },
          gcs: { bucket: '', location: 'us-central1', public_access_prevention: true },
          vertex_ai: { llm_model_id: 'gemini-3.1-pro-preview', llm_location: 'global', img_model_id: '', img_location: 'us-central1', video_model_id: '', video_location: 'us-central1' },
        },
        upload: { max_file_size_mb: 20 },
      }))
      .finally(() => setLoading(false));
    fetchHealth().then(h => setHealth(h.services || {})).catch(() => {});
    const interval = setInterval(() => {
      fetchHealth().then(h => setHealth(h.services || {})).catch(() => {});
    }, 30000);
    return () => clearInterval(interval);
  }, []);

  const handleSave = async () => {
    if (!settings) return;
    setSaveStatus('saving');
    setErrorMsg('');
    try {
      const updated = await updateSettings(settings);
      setSettings(updated);
      setSaveStatus('saved');
      setTimeout(() => setSaveStatus('idle'), 2500);
    } catch (err: unknown) {
      setSaveStatus('error');
      setErrorMsg(err instanceof Error ? err.message : 'Failed to save settings');
      setTimeout(() => setSaveStatus('idle'), 4000);
    }
  };

  if (loading || !settings) {
    return (
      <div className="p-8 max-w-[800px] mx-auto">
        <div className="flex items-center gap-2 text-zinc-500 text-sm">
          <Loader2 size={16} className="animate-spin" />
          Loading settings...
        </div>
      </div>
    );
  }

  return (
    <div className="p-8 max-w-[800px] mx-auto">
      <header className="flex items-center justify-between mb-8">
        <div>
          <div className="flex items-center gap-2.5 mb-1">
            <Settings size={20} className="text-cyan-400" />
            <h2 className="text-2xl font-bold tracking-tight text-white">Settings</h2>
          </div>
          <p className="text-xs text-zinc-500">Infrastructure connections and service configuration.</p>
        </div>

        {/* Save Button */}
        <button
          onClick={handleSave}
          disabled={saveStatus === 'saving'}
          className="flex items-center gap-2 px-5 py-2 rounded-lg text-sm font-medium transition-all cursor-pointer border"
          style={{
            background: saveStatus === 'saved' ? '#052e16' : saveStatus === 'error' ? '#450a0a' : 'linear-gradient(135deg, #0e7490, #164e63)',
            borderColor: saveStatus === 'saved' ? '#052e1660' : saveStatus === 'error' ? '#450a0a60' : '#0e749060',
            color: saveStatus === 'saved' ? '#4ade80' : saveStatus === 'error' ? '#fb7185' : '#e0f2fe',
          }}
        >
          {saveStatus === 'saving' && <Loader2 size={14} className="animate-spin" />}
          {saveStatus === 'saved' && <Check size={14} />}
          {saveStatus === 'error' && <AlertCircle size={14} />}
          {saveStatus === 'idle' && <Save size={14} />}
          {saveStatus === 'saving' ? 'Saving...' : saveStatus === 'saved' ? 'Saved' : saveStatus === 'error' ? 'Error' : 'Save All'}
        </button>
      </header>

      {errorMsg && (
        <div className="mb-6 p-3 rounded-lg border border-red-500/20 text-xs text-red-400" style={{ background: '#450a0a20' }}>
          {errorMsg}
        </div>
      )}

      {/* PostgreSQL */}
      <ConfigSection icon={<Database size={16} />} title="PostgreSQL" desc="Business data — ad accounts, tasks, campaigns" color="#38bdf8" status={health.postgres}>
        <div className="grid grid-cols-2 gap-3">
          <ConfigInput label="Host" value={settings.postgres.host}
            onChange={v => setSettings({ ...settings, postgres: { ...settings.postgres, host: v } })} />
          <ConfigInput label="Port" value={String(settings.postgres.port)} type="number"
            onChange={v => setSettings({ ...settings, postgres: { ...settings.postgres, port: parseInt(v) || 5432 } })} />
          <ConfigInput label="User" value={settings.postgres.user}
            onChange={v => setSettings({ ...settings, postgres: { ...settings.postgres, user: v } })} />
          <ConfigInput label="Password" value={settings.postgres.password} type="password"
            onChange={v => setSettings({ ...settings, postgres: { ...settings.postgres, password: v } })} />
          <ConfigInput label="Database" value={settings.postgres.dbname} className="col-span-2"
            onChange={v => setSettings({ ...settings, postgres: { ...settings.postgres, dbname: v } })} />
        </div>
      </ConfigSection>

      {/* Elasticsearch */}
      <ConfigSection icon={<Search size={16} />} title="Elasticsearch" desc="Creatives index and search" color="#c084fc" status={health.elasticsearch}>
        <ConfigInput label="URL" value={settings.elasticsearch.url}
          onChange={v => setSettings({ ...settings, elasticsearch: { ...settings.elasticsearch, url: v } })}
          placeholder="http://localhost:9200" />
      </ConfigSection>

      {/* Google Cloud */}
      <ConfigSection icon={<Cloud size={16} />} title="Google Cloud" desc="GCP project, credentials, BigQuery & Vertex AI" color="#fbbf24" status={health.gcs}>
        <div className="grid grid-cols-2 gap-3">
          <ConfigInput label="Project ID" value={settings.google_cloud.project_id}
            onChange={v => setSettings({ ...settings, google_cloud: { ...settings.google_cloud, project_id: v } })}
            placeholder="your-gcp-project-id" />
          <ConfigInput label="Credentials Path" value={settings.google_cloud.credentials_path}
            onChange={v => setSettings({ ...settings, google_cloud: { ...settings.google_cloud, credentials_path: v } })}
            placeholder="Leave empty to use ADC" />
        </div>

        {/* BigQuery subsection */}
        <div className="mt-5 pt-4 border-t border-zinc-800/40">
          <SubsectionHeader label="BigQuery" status={health.bigquery} />
          <ConfigInput label="Dataset" value={settings.google_cloud.bigquery.dataset}
            onChange={v => setSettings({ ...settings, google_cloud: { ...settings.google_cloud, bigquery: { ...settings.google_cloud.bigquery, dataset: v } } })} />
        </div>

        {/* Cloud Storage subsection */}
        <div className="mt-5 pt-4 border-t border-zinc-800/40">
          <SubsectionHeader label="Cloud Storage" status={health.gcs} />
          <div className="grid grid-cols-2 gap-3">
            <ConfigInput label="Bucket" value={settings.google_cloud.gcs.bucket}
              onChange={v => setSettings({ ...settings, google_cloud: { ...settings.google_cloud, gcs: { ...settings.google_cloud.gcs, bucket: v } } })}
              placeholder="my-mobius-bucket" />
            <ConfigInput label="Location" value={settings.google_cloud.gcs.location}
              onChange={v => setSettings({ ...settings, google_cloud: { ...settings.google_cloud, gcs: { ...settings.google_cloud.gcs, location: v } } })}
              placeholder="us-central1" />
            <ConfigToggle label="Public Access Prevention" checked={settings.google_cloud.gcs.public_access_prevention}
              onChange={v => setSettings({ ...settings, google_cloud: { ...settings.google_cloud, gcs: { ...settings.google_cloud.gcs, public_access_prevention: v } } })}
              className="col-span-2" />
          </div>
        </div>

        {/* Vertex AI subsection */}
        <div className="mt-5 pt-4 border-t border-zinc-800/40">
          <SubsectionHeader label="Vertex AI Models" status={health.llm} />
          <ModelRegistry />
        </div>
      </ConfigSection>

      {/* Upload Limits */}
      <ConfigSection icon={<Upload size={16} />} title="Upload Limits" desc="File size limits for chat attachments" color="#a1a1aa">
        <ConfigInput label="Max File Size (MB)" value={String(settings.upload.max_file_size_mb)} type="number"
          onChange={v => setSettings({ ...settings, upload: { ...settings.upload, max_file_size_mb: parseInt(v) || 20 } })} />
      </ConfigSection>

      {/* Ad Account Connections */}
      <section className="mb-8">
        <h3 className="text-sm font-semibold text-zinc-300 mb-4">Ad Account Connections</h3>
        <div
          className="rounded-xl border border-zinc-800/40 divide-y divide-zinc-800/30"
          style={{ background: '#111114' }}
        >
          {AD_PLATFORMS.map(platform => (
            <div key={platform.name} className="flex items-center justify-between p-5">
              <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg border border-zinc-800/50" style={{ background: '#18181b' }}>
                  <Link2 size={16} className="text-zinc-500" />
                </div>
                <div>
                  <p className="text-sm font-medium text-zinc-300">{platform.name}</p>
                  <p className="text-[11px] text-zinc-600">{platform.desc}</p>
                </div>
              </div>
              <button
                className="px-4 py-1.5 rounded-lg text-xs font-medium border transition-colors cursor-pointer text-cyan-400"
                style={{ background: '#0e749010', borderColor: '#0e749030' }}
              >
                Connect
              </button>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function StatusDot({ status }: { status?: ServiceStatus }) {
  if (!status) return <span className="block w-2 h-2 rounded-full bg-zinc-700" title="Unknown" />;
  if (status.status === 'ok') return <span className="block w-2 h-2 rounded-full bg-emerald-400" title="Connected" />;
  if (status.status === 'unconfigured') return <span className="block w-2 h-2 rounded-full bg-amber-400" title={status.error || 'Not configured'} />;
  return <span className="block w-2 h-2 rounded-full bg-red-400" title={status.error || 'Unavailable'} />;
}

function SubsectionHeader({ label, status }: { label: string; status?: ServiceStatus }) {
  return (
    <div className="flex items-center gap-2 mb-3">
      <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-wider">{label}</p>
      <StatusDot status={status} />
      {status?.error && status.status !== 'ok' && (
        <span className="text-[9px] text-zinc-600">{status.error}</span>
      )}
    </div>
  );
}

function ConfigSection({ icon, title, desc, color, children, status }: {
  icon: React.ReactNode; title: string; desc: string; color: string; children: React.ReactNode; status?: ServiceStatus;
}) {
  return (
    <section className="mb-8">
      <div className="flex items-center gap-2.5 mb-4">
        <div className="p-1.5 rounded-lg border border-zinc-800/50" style={{ background: '#18181b' }}>
          <span style={{ color }}>{icon}</span>
        </div>
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold text-zinc-300">{title}</h3>
            <StatusDot status={status} />
            {status?.error && status.status !== 'ok' && (
              <span className="text-[9px] text-zinc-600">{status.error}</span>
            )}
          </div>
          <p className="text-[10px] text-zinc-600">{desc}</p>
        </div>
      </div>
      <div className="rounded-xl border border-zinc-800/40 p-5" style={{ background: '#111114' }}>
        {children}
      </div>
    </section>
  );
}

function ConfigInput({ label, value, onChange, placeholder, type = 'text', className = '', status }: {
  label: string; value: string; onChange: (v: string) => void;
  placeholder?: string; type?: string; className?: string; status?: ServiceStatus;
}) {
  return (
    <div className={className}>
      <div className="flex items-center gap-1.5 mb-1.5 px-0.5">
        <label className="text-[10px] font-mono text-zinc-600 uppercase">{label}</label>
        {status && <StatusDot status={status} />}
      </div>
      <input
        type={type}
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full text-xs text-zinc-300 rounded-lg px-3 py-2.5 outline-none border border-zinc-800/50 transition-colors focus:border-cyan-500/30 placeholder:text-zinc-700 font-mono"
        style={{ background: '#09090b' }}
      />
    </div>
  );
}

function ConfigToggle({ label, checked, onChange, className = '' }: {
  label: string; checked: boolean; onChange: (v: boolean) => void; className?: string;
}) {
  return (
    <div className={`flex items-center justify-between py-1 ${className}`}>
      <label className="text-[10px] font-mono text-zinc-600 uppercase px-0.5">{label}</label>
      <button
        type="button"
        onClick={() => onChange(!checked)}
        className="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border border-zinc-700/50 transition-colors"
        style={{ background: checked ? '#0e7490' : '#27272a' }}
      >
        <span
          className="pointer-events-none inline-block h-4 w-4 rounded-full shadow-sm transition-transform"
          style={{
            background: checked ? '#e0f2fe' : '#71717a',
            transform: checked ? 'translateX(16px)' : 'translateX(0)',
          }}
        />
      </button>
    </div>
  );
}

const MODEL_TYPES = ['llm', 'image', 'video'] as const;
const TYPE_COLORS: Record<string, string> = { llm: '#38bdf8', image: '#c084fc', video: '#fbbf24' };

function ModelRegistry() {
  const [models, setModels] = useState<VertexModel[]>([]);
  const [showAdd, setShowAdd] = useState(false);
  const [newModel, setNewModel] = useState({ id: '', name: '', model_id: '', location: 'global', type: 'llm' as string });

  useEffect(() => { listModels().then(setModels).catch(() => {}); }, []);

  const handleAdd = async () => {
    if (!newModel.id || !newModel.model_id || !newModel.type) return;
    const m = await addModel(newModel as VertexModel);
    setModels(prev => [...prev, m]);
    setNewModel({ id: '', name: '', model_id: '', location: 'global', type: 'llm' });
    setShowAdd(false);
  };

  const handleRemove = async (id: string) => {
    await removeModel(id);
    setModels(prev => prev.filter(m => m.id !== id));
  };

  return (
    <div>
      {models.length === 0 ? (
        <p className="text-xs text-zinc-600 py-2">No models registered.</p>
      ) : (
        <div className="space-y-1.5 mb-3">
          {models.map(m => (
            <div key={m.id} className="flex items-center gap-3 px-3 py-2 rounded-lg border border-zinc-800/40" style={{ background: '#09090b' }}>
              <span className="px-1.5 py-0.5 rounded text-[9px] font-bold uppercase" style={{ color: TYPE_COLORS[m.type] || '#a1a1aa', background: `${TYPE_COLORS[m.type] || '#a1a1aa'}15`, border: `1px solid ${TYPE_COLORS[m.type] || '#a1a1aa'}30` }}>
                {m.type}
              </span>
              <div className="flex-1 min-w-0">
                <p className="text-xs font-medium text-zinc-300 truncate">{m.name || m.model_id}</p>
                <p className="text-[10px] text-zinc-600 truncate">{m.model_id} · {m.location}</p>
              </div>
              <button onClick={() => handleRemove(m.id)} className="p-1 rounded text-zinc-700 hover:text-red-400 hover:bg-red-500/10 cursor-pointer transition-colors">
                <Trash2 size={12} />
              </button>
            </div>
          ))}
        </div>
      )}

      {showAdd ? (
        <div className="rounded-lg border border-zinc-800/40 p-3 space-y-2" style={{ background: '#09090b' }}>
          <div className="grid grid-cols-2 gap-2">
            <input value={newModel.id} onChange={e => setNewModel({ ...newModel, id: e.target.value })}
              placeholder="Unique ID (e.g. gemini-pro)" className="text-xs text-zinc-300 rounded-lg px-2.5 py-2 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700 font-mono" style={{ background: '#111114' }} />
            <input value={newModel.name} onChange={e => setNewModel({ ...newModel, name: e.target.value })}
              placeholder="Display name" className="text-xs text-zinc-300 rounded-lg px-2.5 py-2 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700" style={{ background: '#111114' }} />
            <input value={newModel.model_id} onChange={e => setNewModel({ ...newModel, model_id: e.target.value })}
              placeholder="Model ID (e.g. gemini-3.1-pro-preview)" className="text-xs text-zinc-300 rounded-lg px-2.5 py-2 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700 font-mono" style={{ background: '#111114' }} />
            <input value={newModel.location} onChange={e => setNewModel({ ...newModel, location: e.target.value })}
              placeholder="Location" className="text-xs text-zinc-300 rounded-lg px-2.5 py-2 outline-none border border-zinc-800/50 focus:border-cyan-500/30 placeholder:text-zinc-700" style={{ background: '#111114' }} />
          </div>
          <div className="flex items-center gap-2">
            <select value={newModel.type} onChange={e => setNewModel({ ...newModel, type: e.target.value })}
              className="text-xs text-zinc-300 rounded-lg px-2.5 py-2 outline-none border border-zinc-800/50 focus:border-cyan-500/30 cursor-pointer" style={{ background: '#111114' }}>
              {MODEL_TYPES.map(t => <option key={t} value={t}>{t.toUpperCase()}</option>)}
            </select>
            <div className="flex-1" />
            <button onClick={() => setShowAdd(false)} className="px-3 py-1.5 rounded-lg text-xs text-zinc-500 hover:text-zinc-300 cursor-pointer transition-colors">Cancel</button>
            <button onClick={handleAdd} disabled={!newModel.id || !newModel.model_id}
              className="px-3 py-1.5 rounded-lg text-xs font-medium text-white cursor-pointer disabled:opacity-40 transition-colors"
              style={{ background: 'linear-gradient(135deg, #0e7490, #164e63)' }}>Add</button>
          </div>
        </div>
      ) : (
        <button onClick={() => setShowAdd(true)}
          className="flex items-center gap-1.5 text-xs text-cyan-400 hover:text-cyan-300 cursor-pointer transition-colors">
          <Plus size={12} /> Register Model
        </button>
      )}
    </div>
  );
}
