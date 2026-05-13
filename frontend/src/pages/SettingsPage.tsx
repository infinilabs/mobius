import { useState, useEffect } from 'react';
import {
  Settings, Database, Search, Cloud, Link2,
  Save, Check, AlertCircle, Loader2,
} from 'lucide-react';
import { fetchSettings, updateSettings } from '../api';
import type { SettingsData } from '../types';

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

  useEffect(() => {
    fetchSettings()
      .then(setSettings)
      .catch(() => setSettings({
        postgres: { host: 'localhost', port: 5432, user: 'mobius', password: 'mobius', dbname: 'mobius' },
        elasticsearch: { url: 'http://localhost:9200' },
        bigquery: { project_id: '', dataset: 'mobius', credentials_path: '', model_id: 'gemini-3.1-pro-preview', location: 'global' },
      }))
      .finally(() => setLoading(false));
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
      <ConfigSection icon={<Database size={16} />} title="PostgreSQL" desc="Business data — ad accounts, tasks, campaigns" color="#38bdf8">
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
      <ConfigSection icon={<Search size={16} />} title="Elasticsearch" desc="Creatives index and search" color="#c084fc">
        <ConfigInput label="URL" value={settings.elasticsearch.url}
          onChange={v => setSettings({ ...settings, elasticsearch: { ...settings.elasticsearch, url: v } })}
          placeholder="http://localhost:9200" />
      </ConfigSection>

      {/* BigQuery */}
      <ConfigSection icon={<Cloud size={16} />} title="BigQuery" desc="Analytical data — reporting and metrics" color="#fbbf24">
        <div className="grid grid-cols-2 gap-3">
          <ConfigInput label="Project ID" value={settings.bigquery.project_id}
            onChange={v => setSettings({ ...settings, bigquery: { ...settings.bigquery, project_id: v } })}
            placeholder="your-gcp-project-id" />
          <ConfigInput label="Dataset" value={settings.bigquery.dataset}
            onChange={v => setSettings({ ...settings, bigquery: { ...settings.bigquery, dataset: v } })} />
          <ConfigInput label="Model ID" value={settings.bigquery.model_id}
            onChange={v => setSettings({ ...settings, bigquery: { ...settings.bigquery, model_id: v } })}
            placeholder="gemini-3.1-pro-preview" />
          <ConfigInput label="Location" value={settings.bigquery.location}
            onChange={v => setSettings({ ...settings, bigquery: { ...settings.bigquery, location: v } })}
            placeholder="global" />
          <ConfigInput label="Credentials Path" value={settings.bigquery.credentials_path} className="col-span-2"
            onChange={v => setSettings({ ...settings, bigquery: { ...settings.bigquery, credentials_path: v } })}
            placeholder="Leave empty to use ADC" />
        </div>
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

function ConfigSection({ icon, title, desc, color, children }: {
  icon: React.ReactNode; title: string; desc: string; color: string; children: React.ReactNode;
}) {
  return (
    <section className="mb-8">
      <div className="flex items-center gap-2.5 mb-4">
        <div className="p-1.5 rounded-lg border border-zinc-800/50" style={{ background: '#18181b' }}>
          <span style={{ color }}>{icon}</span>
        </div>
        <div>
          <h3 className="text-sm font-semibold text-zinc-300">{title}</h3>
          <p className="text-[10px] text-zinc-600">{desc}</p>
        </div>
      </div>
      <div className="rounded-xl border border-zinc-800/40 p-5" style={{ background: '#111114' }}>
        {children}
      </div>
    </section>
  );
}

function ConfigInput({ label, value, onChange, placeholder, type = 'text', className = '' }: {
  label: string; value: string; onChange: (v: string) => void;
  placeholder?: string; type?: string; className?: string;
}) {
  return (
    <div className={className}>
      <label className="text-[10px] font-mono text-zinc-600 uppercase block mb-1.5 px-0.5">{label}</label>
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
