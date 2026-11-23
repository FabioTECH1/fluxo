const releaseTagPattern = /^v\d+\.\d+\.\d+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;

export const onRequest: PagesFunction = ({ request, params }) => {
  const headers = {
    'Content-Type': 'text/plain; charset=utf-8',
    'X-Content-Type-Options': 'nosniff',
    'Cache-Control': 'no-store',
  };

  if (request.method !== 'GET' && request.method !== 'HEAD') {
    return new Response('Method not allowed', {
      status: 405,
      headers: { ...headers, Allow: 'GET, HEAD' },
    });
  }

  const tag = params.tag;
  if (typeof tag !== 'string' || tag.length > 100 || tag !== tag.trim() || !releaseTagPattern.test(tag)) {
    return new Response(request.method === 'HEAD' ? null : 'Invalid release tag', {
      status: 404,
      headers,
    });
  }

  // Redirect to the original tagged asset; never substitute the latest script.
  return new Response(null, {
    status: 302,
    headers: {
      ...headers,
      'Cache-Control': 'public, max-age=300',
      Location: `https://github.com/FabioTECH1/fluxo/releases/download/${encodeURIComponent(tag)}/install.sh`,
    },
  });
};
