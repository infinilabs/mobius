type LogLevel = 'debug' | 'info' | 'warn' | 'error';

const LEVEL_PRIORITY: Record<LogLevel, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3,
};

const LEVEL_STYLES: Record<LogLevel, string> = {
  debug: 'color: #38bdf8',
  info: 'color: #4ade80',
  warn: 'color: #fbbf24',
  error: 'color: #fb7185',
};

class Logger {
  private minLevel: LogLevel;

  constructor() {
    this.minLevel = import.meta.env.DEV ? 'debug' : 'warn';
  }

  private shouldLog(level: LogLevel): boolean {
    return LEVEL_PRIORITY[level] >= LEVEL_PRIORITY[this.minLevel];
  }

  private format(level: LogLevel, component: string, message: string, data?: Record<string, unknown>) {
    if (!this.shouldLog(level)) return;

    const ts = new Date().toISOString();
    const prefix = `%c[${level.toUpperCase()}]%c ${ts} [${component}]`;
    const style = LEVEL_STYLES[level];

    if (data && Object.keys(data).length > 0) {
      console[level === 'debug' ? 'debug' : level === 'error' ? 'error' : level === 'warn' ? 'warn' : 'info'](
        `${prefix} ${message}`, style, 'color: inherit', data,
      );
    } else {
      console[level === 'debug' ? 'debug' : level === 'error' ? 'error' : level === 'warn' ? 'warn' : 'info'](
        `${prefix} ${message}`, style, 'color: inherit',
      );
    }
  }

  debug(component: string, message: string, data?: Record<string, unknown>) {
    this.format('debug', component, message, data);
  }

  info(component: string, message: string, data?: Record<string, unknown>) {
    this.format('info', component, message, data);
  }

  warn(component: string, message: string, data?: Record<string, unknown>) {
    this.format('warn', component, message, data);
  }

  error(component: string, message: string, data?: Record<string, unknown>) {
    this.format('error', component, message, data);
  }
}

export const log = new Logger();
