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
  const latestStatus = ref<DeployStatus>('');
  const lastKnownDeployId = ref<number | null>(null);
  const awaitingNewDeployment = ref(false);
  const deploying = computed(() => isActiveStatus(latestStatus.value));
  const deploySignal = ref(0);

  let fastPoll: number | null = null;
  let backgroundPoll: number | null = null;
  let notifyOnStatusChange = false;
  let dismissClickHandler: (() => void) | null = null;

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
    try {
      const data = await apiClient.getSiteDeployments(siteId.value, 1, true);
      const latest = data?.data?.[0];
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
    if (!siteId.value || deploying.value) return;
    notifyOnStatusChange = true;
    awaitingNewDeployment.value = true;
    latestStatus.value = 'pending';
    setDeploying();
    try {
      await apiClient.triggerSiteDeploy(siteId.value);
      deploySignal.value++;
      await pollLatest({ manual: true, notify: true });
      if (!fastPoll) fastPoll = window.setInterval(() => pollLatest({ notify: true }).catch(() => {}), 2000);
    } catch (e: any) {
      awaitingNewDeployment.value = false;
      latestStatus.value = '';
      clearFastPoll();
      resetFavicon();
      addToast(e.message || 'Failed to trigger deployment', 'error');
    }
  };

  return {
    siteId,
    latestDeployment,
    latestStatus,
    deploying,
    deploySignal,
    setSite,
    pollLatest,
    startBackgroundPolling,
    stopPolling,
    triggerDeploy,
  };
});
