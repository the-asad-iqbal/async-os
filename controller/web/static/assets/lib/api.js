// API client. Every state-changing request carries the anti-CSRF header the
// server requires; a 401 sends the user back to sign-in.

export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

let onUnauthorized = () => {};
export function setUnauthorizedHandler(fn) { onUnauthorized = fn; }

export async function api(method, path, body) {
  const options = { method, headers: { Accept: 'application/json' }, credentials: 'same-origin' };
  if (method !== 'GET') options.headers['X-AsyncOS-CSRF'] = '1';
  if (body !== undefined) {
    options.headers['Content-Type'] = 'application/json';
    options.body = JSON.stringify(body);
  }
  let response;
  try {
    response = await fetch(`/api/v1${path}`, options);
  } catch {
    throw new ApiError(0, 'The controller cannot be reached. Check the network connection.');
  }
  if (response.status === 204) return null;
  let data = null;
  try { data = await response.json(); } catch { /* empty body */ }
  if (!response.ok) {
    if (response.status === 401 && !path.startsWith('/auth/login') && !path.startsWith('/setup')) onUnauthorized();
    throw new ApiError(response.status, data?.error || `Request failed (${response.status})`);
  }
  return data;
}

export const get = (path) => api('GET', path);
export const post = (path, body) => api('POST', path, body ?? {});
export const put = (path, body) => api('PUT', path, body);
export const patch = (path, body) => api('PATCH', path, body);
export const del = (path) => api('DELETE', path);
