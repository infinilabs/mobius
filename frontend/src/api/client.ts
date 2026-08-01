import axios from 'axios';

// Shared API token: when the backend sets server.api_token (or MOBIUS_API_TOKEN),
// every /api/* request must carry it. Axios calls send a Bearer header; the
// SameSite=Strict cookie covers <img>/<video>/<iframe> loads of /api/... URLs,
// which cannot set headers. Store the token once via:
//   localStorage.setItem('mobius_api_token', '<token>')
const apiToken = localStorage.getItem('mobius_api_token');
if (apiToken) {
  axios.defaults.headers.common['Authorization'] = `Bearer ${apiToken}`;
  document.cookie = `mobius_token=${encodeURIComponent(apiToken)}; path=/; SameSite=Strict`;
}
