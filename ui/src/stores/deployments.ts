import { computed, ref } from 'vue';
import { defineStore } from 'pinia';
import { apiClient } from '../api/client';
import { useFavicon } from '../composables/useFavicon';
import { useToast } from '../composables/useToast';

type DeployStatus = 'pending' | 'running' | 'success' | 'failed' | string;

const isActiveStatus = (status: DeployStatus) => status === 'running' || status === 'pending';

export const useDeploymentsStore = defineStore('deployments', () => {
  const siteId = ref<string | number | null>(null);
  const latestDeployment = ref<any>(null);
  const unresolvedFailure = ref<any>(null);
  const latestStatus = ref<DeployStatus>('');
  const lastKnownDeployId = ref<number | null>(null);
  const awaitingNewDeployment = ref(false);
  const deploying = computed(() => isActiveStatus(latestStatus.value));
  const deploySignal = ref(0);

  let fastPoll: number | null = null;
  let backgroundPoll: number | null = null;
  let notifyOnStatusChange = false;
  let dismissClickHandler: (() => void) | null = null;
  let pollRequestVersion = 0;

  const { addToast } = useToast();
  const { setDeploying, setSuccess, setFailed, reset: resetFavicon } = useFavicon();

  const removeDismissOnClick = () => {
    if (dismissClickHandler) {
      document.removeEventListener('click', dismissClickHandler);
      dismissClickHandler = null;
    }
  };

  const addDismissOnClick = () => {
    removeDismissOnClick();
    const handler = () => {
      resetFavicon();
      removeDismissOnClick();
    };
    document.addEventListener('click', handler, { once: true });
    dismissClickHandler = handler;
  };

  const clearFastPoll = () => {
    if (fastPoll) {
      window.clearInterval(fastPoll);
      fastPoll = null;
    }
  };

  const clearBackgroundPoll = () => {
    if (backgroundPoll) {
      window.clearInterval(backgroundPoll);
      backgroundPoll = null;
    }
  };

  const stopPolling = () => {
    pollRequestVersion++;
    clearFastPoll();
    clearBackgroundPoll();
    removeDismissOnClick();
    notifyOnStatusChange = false;
  };

  const setSite = (id: string | number | null) => {
    if (siteId.value === id) return;
    stopPolling();
    siteId.value = id;
    latestDeployment.value = null;
    unresolvedFailure.value = null;
    latestStatus.value = '';
    lastKnownDeployId.value = null;
    awaitingNewDeployment.value = false;
    resetFavicon();
  };

  const markTerminalStatus = (status: DeployStatus) => {
    if (status === 'success') {
      setSuccess();
      addDismissOnClick();
    } else if (status === 'failed') {
      setFailed();
      addDismissOnClick();
    } else {
      resetFavicon();
    }
  };

  const pollLatest = async (options: { manual?: boolean; notify?: boolean } = {}) => {
    if (!siteId.value) return null;
    const requestedSiteId = siteId.value;
    const requestVersion = ++pollRequestVersion;
    try {
      const data = await apiClient.getSiteDeployments(requestedSiteId, 1, true);
      if (siteId.value !== requestedSiteId || requestVersion !== pollRequestVersion) return null;
      unresolvedFailure.value = data?.unresolved_failure || null;
      const latest = data?.data?.find((deployment: any) => deployment.trigger_source !== 'repo_sync');
      if (!latest) {
        latestDeployment.value = null;
        latestStatus.value = awaitingNewDeployment.value ? 'pending' : '';
        clearFastPoll();
        if (awaitingNewDeployment.value) {
          setDeploying();
          if (!fastPoll) fastPoll = window.setInterval(() => pollLatest({ notify: options.notify }).catch(() => {}), 2000);
        } else {
          resetFavicon();
        }
        return null;
      }

      const previousStatus = latestStatus.value;
      const previousId = lastKnownDeployId.value;
      const previousWasActive = isActiveStatus(previousStatus);
      const latestIsActive = isActiveStatus(latest.status);
      const firstPoll = previousId === null;

      if (awaitingNewDeployment.value && previousId !== null && latest.id <= previousId && !latestIsActive) {
        latestDeployment.value = latest;
        latestStatus.value = 'pending';
        setDeploying();
        if (!fastPoll) fastPoll = window.setInterval(() => pollLatest({ notify: options.notify }).catch(() => {}), 2000);
        return latest;
      }

      if (awaitingNewDeployment.value && (latest.id > (previousId || 0) || latestIsActive)) {
        awaitingNewDeployment.value = false;
      }

      latestDeployment.value = latest;
      latestStatus.value = latest.status;

      if (firstPoll) {
        lastKnownDeployId.value = latest.id;
        if (latestIsActive) {
          setDeploying();
          if (!fastPoll) fastPoll = window.setInterval(() => pollLatest({ notify: notifyOnStatusChange }).catch(() => {}), 2000);
        } else {
          resetFavicon();
        }
        return latest;
      }

      if (latestIsActive) {
        setDeploying();
        if (!previousWasActive && latest.id > (previousId || 0) && options.notify) {
          addToast('Auto-deployment started', 'info');
          deploySignal.value++;
        }
        lastKnownDeployId.value = latest.id;
        if (!fastPoll) fastPoll = window.setInterval(() => pollLatest({ notify: notifyOnStatusChange }).catch(() => {}), 2000);
        return latest;
      }

      if (previousWasActive || options.manual || latest.id > (previousId || 0)) {
        if (latest.status === 'success' && options.notify) {
          addToast('Deployment finished successfully', 'success');
        } else if (latest.status === 'failed' && options.notify) {
          addToast('Deployment failed', 'error');
        }
        markTerminalStatus(latest.status);
        lastKnownDeployId.value = latest.id;
      }

      clearFastPoll();
      return latest;
    } catch (e) {
      if (siteId.value !== requestedSiteId || requestVersion !== pollRequestVersion) return null;
      clearFastPoll();
      resetFavicon();
      throw e;
    }
  };

  const startBackgroundPolling = (id: string | number, notify = false) => {
    setSite(id);
    notifyOnStatusChange = notifyOnStatusChange || notify;
    pollLatest({ notify: notifyOnStatusChange }).catch(() => {});
    if (!backgroundPoll) {
      backgroundPoll = window.setInterval(() => pollLatest({ notify: notifyOnStatusChange }).catch(() => {}), 10000);
    }
  };

  const triggerDeploy = async () => {
    if (!siteId.value || deploying.value) return null;
    const requestedSiteId = siteId.value;
    const previousStatus = latestStatus.value;
    notifyOnStatusChange = true;
    awaitingNewDeployment.value = true;
    latestStatus.value = 'pending';
    setDeploying();
    try {
      const result = await apiClient.triggerSiteDeploy(requestedSiteId);
      const deploymentId = Number(result?.deployment_id);
      const acceptedDeploymentId = Number.isInteger(deploymentId) && deploymentId > 0 ? deploymentId : 0;
      if (siteId.value !== requestedSiteId) return acceptedDeploymentId;
      deploySignal.value++;
      pollLatest({ manual: true, notify: true }).catch(() => {});
      if (!fastPoll) fastPoll = window.setInterval(() => pollLatest({ notify: true }).catch(() => {}), 2000);
      return acceptedDeploymentId;
    } catch (e: any) {
      if (siteId.value === requestedSiteId) {
        awaitingNewDeployment.value = false;
        latestStatus.value = previousStatus;
        clearFastPoll();
        resetFavicon();
      }
      addToast(e.message || 'Failed to trigger deployment', 'error');
      return null;
    }
  };

  const dismissFailure = async (deploymentId: number) => {
    if (!siteId.value) return false;
    const requestedSiteId = siteId.value;
    try {
      await apiClient.dismissDeploymentFailure(requestedSiteId, deploymentId);
      if (siteId.value !== requestedSiteId) return true;
      pollRequestVersion++;
      if (unresolvedFailure.value?.id === deploymentId) {
        unresolvedFailure.value = null;
      }
      return true;
    } catch (e: any) {
      addToast(e.message || 'Failed to dismiss deployment error', 'error');
      return false;
    }
  };

  return {
    siteId,
    latestDeployment,
    unresolvedFailure,
    latestStatus,
    deploying,
    deploySignal,
    setSite,
    pollLatest,
    startBackgroundPolling,
    stopPolling,
    triggerDeploy,
    dismissFailure,
  };
});
