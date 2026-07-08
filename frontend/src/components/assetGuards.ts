import type { ProjectAsset } from '../types';

// --- Asset type guards (shared by AssetPreview, ProjectsPage, CreativesPage) ---
// Check playable BEFORE text — a playable ad's mime is text/html.
export function isPlayableAsset(a: ProjectAsset): boolean {
  return a.tags?.includes('playable') || a.mime_type === 'text/html';
}
export function isImageAsset(a: ProjectAsset): boolean {
  return a.content_type === 'image' || a.mime_type?.startsWith('image/');
}
export function isVideoAsset(a: ProjectAsset): boolean {
  return a.content_type === 'video' || a.mime_type?.startsWith('video/');
}
export function isAudioAsset(a: ProjectAsset): boolean {
  return a.content_type === 'audio' || a.mime_type?.startsWith('audio/');
}
export function isPdfAsset(a: ProjectAsset): boolean {
  return a.content_type === 'pdf' || a.mime_type === 'application/pdf';
}
export function isTextAsset(a: ProjectAsset): boolean {
  return !isPlayableAsset(a) && (a.content_type === 'text' || a.content_type === 'code' || a.mime_type?.startsWith('text/'));
}
export function isPreviewable(a: ProjectAsset): boolean {
  return isImageAsset(a) || isVideoAsset(a) || isAudioAsset(a) || isPlayableAsset(a) || isPdfAsset(a) || isTextAsset(a);
}
// Only image / video / playable assets can be promoted to the Creatives library.
export function isCreativeEligible(a: ProjectAsset): boolean {
  return isImageAsset(a) || isVideoAsset(a) || isPlayableAsset(a);
}
