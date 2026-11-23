export interface WordPressCronState {
  enabled: boolean;
  phase: 'off' | 'enabling' | 'enabled' | 'disabling' | 'attention';
  cron_id?: number;
  message?: string;
}
