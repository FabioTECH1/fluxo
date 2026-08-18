const githubLatestReleaseURL = 'https://api.github.com/repos/FabioTECH1/fluxo/releases/latest';
const releaseURLPathPrefix = '/FabioTECH1/fluxo/releases/tag/';
const maxUpstreamBodyBytes = 64 * 1024;

type GitHubRelease = {
  tag_name?: unknown;
  html_url?: unknown;
  published_at?: unknown;
};

const jsonResponse = (body: Record<string, unknown>, status: number, cacheControl: string) => new Response(
  JSON.stringify(body),
  {
    status,
    headers: {
      'Cache-Control': cacheControl,
      'Content-Type': 'application/json; charset=utf-8',
      'X-Content-Type-Options': 'nosniff',
    },
  },
);

const readLimitedBody = async (response: Response): Promise<string> => {
  if (!response.body) return '';

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > maxUpstreamBodyBytes) {
        await reader.cancel();
        throw new Error('GitHub release response exceeded the size limit');
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  const body = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(body);
};

const parseRelease = (value: unknown) => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error('GitHub returned invalid release metadata');
  }
  const payload = value as GitHubRelease;
  if (
    typeof payload.tag_name !== 'string'
    || typeof payload.html_url !== 'string'
    || typeof payload.published_at !== 'string'
  ) {
    throw new Error('GitHub returned incomplete release metadata');
  }

  const tag = payload.tag_name.trim();
  const version = tag.startsWith('v') ? tag.slice(1) : tag;
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version)) {
    throw new Error('GitHub returned an invalid release version');
  }
  const releaseURL = new URL(payload.html_url);
  if (
    releaseURL.origin !== 'https://github.com'
    || releaseURL.pathname !== `${releaseURLPathPrefix}${tag}`
    || releaseURL.search !== ''
    || releaseURL.hash !== ''
  ) {
    throw new Error('GitHub returned an unexpected release URL');
  }
  if (Number.isNaN(Date.parse(payload.published_at))) {
    throw new Error('GitHub returned an invalid release date');
  }

  return {
    version,
    tag,
    published_at: payload.published_at,
    release_url: payload.html_url,
  };
};

export const onRequestGet: PagesFunction = async () => {
  try {
    const upstream = await fetch(githubLatestReleaseURL, {
      headers: {
        Accept: 'application/vnd.github+json',
        'User-Agent': 'Fluxo-Release-Check',
        'X-GitHub-Api-Version': '2022-11-28',
      },
      signal: AbortSignal.timeout(3000),
      cf: {
        cacheEverything: true,
        cacheTtlByStatus: {
          '200-299': 900,
          '404': 60,
          '500-599': 0,
        },
      },
    });

    if (!upstream.ok) {
      throw new Error(`GitHub returned HTTP ${upstream.status}`);
    }

    const rawBody = await readLimitedBody(upstream);
    const parsed: unknown = JSON.parse(rawBody);
    const release = parseRelease(parsed);

    return jsonResponse(
      release,
      200,
      'public, max-age=300, s-maxage=900, stale-while-revalidate=3600',
    );
  } catch {
    return jsonResponse(
      { error: 'Latest release information is temporarily unavailable' },
      502,
      'no-store',
    );
  }
};
