// Match the destination and its children without hiding notices on sibling routes.
export function isRuntimeDestination(path: string, runtime: 'php' | 'node'): boolean {
  const destination = `/runtime/${runtime}`;
  return path === destination || path.startsWith(`${destination}/`);
}
