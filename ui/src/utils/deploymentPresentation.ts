type DeploymentTitle = {
  commit_message?: string | null;
  trigger_source?: string | null;
};

export function deploymentTitle(deployment: DeploymentTitle): string {
  if (deployment.commit_message?.trim()) return deployment.commit_message;

  switch (deployment.trigger_source) {
    case 'github_webhook': return 'Automatic Deployment';
    case 'rollback': return 'Rollback Deployment';
    case 'repo_sync': return 'Repository Sync';
    default: return 'Manual Deployment';
  }
}

export type DeploymentTiming = {
  status?: string;
  started_at?: string | null;
  finished_at?: string | null;
};

// Queue insertion and later edits are not execution boundaries. Older records
// and interrupted attempts intentionally have no complete duration.
export function deploymentDuration(deployment: DeploymentTiming, now = Date.now()): string {
  const start = deployment.started_at ? Date.parse(deployment.started_at) : NaN;
  let end = NaN;
  if (deployment.status === 'running' && !deployment.finished_at) end = now;
  else if (deployment.status === 'success' || deployment.status === 'failed') {
    end = deployment.finished_at ? Date.parse(deployment.finished_at) : NaN;
  }
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return '—';
  const total = Math.floor((end - start) / 1000);
  const seconds = total % 60;
  const minutes = Math.floor(total / 60) % 60;
  const hours = Math.floor(total / 3600);
  return hours ? `${hours}h ${minutes}m ${seconds}s` : total >= 60 ? `${minutes}m ${seconds}s` : `${seconds}s`;
}
