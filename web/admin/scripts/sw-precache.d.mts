// Types of sw-precache.mjs (used by its Vitest test).
export const PLACEHOLDER: string;
export type ViteManifest = Record<string, { file?: string; css?: string[]; assets?: string[] }>;
export type Precache = { version: string; build: string; urls: string[] };
export function manifestFiles(manifest: ViteManifest): Set<string>;
export function precacheList(manifest: ViteManifest, publicFiles?: string[]): string[];
export function buildId(urls: string[]): string;
export function inject(source: string, precache: Precache): string;
export function run(dist: string, version: string): Precache;
