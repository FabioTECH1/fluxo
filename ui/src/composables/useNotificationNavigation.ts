import { useRouter, type RouteLocationRaw } from 'vue-router';

// Notification actions use exact page identity, rather than RouterLink's
// ancestor-active state. Router resolution handles aliases, named routes,
// decoded params, case sensitivity and optional trailing slashes. Distinct
// children, query values and hashes remain useful navigation destinations.
// Query key order is immaterial; repeated-value order is preserved. Redirect
// links and unmatched/external destinations stay visible (resolve doesn't
// follow redirects, and we must not execute redirect functions during render).
export function useNotificationNavigation() {
  const router = useRouter();
  const isCurrentDestination = (to: RouteLocationRaw): boolean => {
    if (typeof to === 'string' && /^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(to)) return false;

    const current = router.currentRoute.value;
    const destination = router.resolve(to);
    const currentRecord = current.matched.at(-1);
    const destinationRecord = destination.matched.at(-1);
    if (!currentRecord || !destinationRecord || destinationRecord.redirect) return false;
    if ((currentRecord.aliasOf || currentRecord) !== (destinationRecord.aliasOf || destinationRecord)) return false;

    const sameValues = (left: Record<string, unknown>, right: Record<string, unknown>) => {
      const keys = Object.keys(left);
      return keys.length === Object.keys(right).length && keys.every(key => {
        const a = left[key];
        const b = right[key];
        const values = (value: unknown) => Array.isArray(value) ? value : [value];
        return Object.hasOwn(right, key) && values(a).length === values(b).length
          && values(a).every((value, index) => value === values(b)[index]);
      });
    };
    return sameValues(current.params, destination.params)
      && sameValues(current.query, destination.query)
      && current.hash === destination.hash;
  };
  return { isCurrentDestination };
}
