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
