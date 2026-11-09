// Hooks of the M12 public marina profile (m12-plan §12, §16.2): the panel draft, its save, publication and photos
// (keys under queryKeys.profile(marinaId); profile.changed travels on marina.changed), the draft preview loaded by
// /p/{slug}?preview=1 with the panel session, and the public-page hooks (keys under ['public', slug]).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, uploadBinary } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type MarinaProfile = S['MarinaProfile'];
export type MarinaProfileInput = S['MarinaProfileInput'];
export type MarinaProfilePreview = S['MarinaProfilePreview'];
export type ProfileText = S['ProfileText'];
export type ProfileHours = S['ProfileHours'];
export type ProfileServices = S['ProfileServices'];
export type ProfilePhoto = S['ProfilePhoto'];
export type ProfilePhotoFile = S['ProfilePhotoFile'];
export type ProfilePoi = S['ProfilePoi'];
export type ProfileWind = S['ProfileWind'];
export type ProfileSeabed = S['ProfileSeabed'];
export type PublicProfile = S['PublicProfile'];
export type PublicProfileMap = S['PublicProfileMap'];
export type PublicPriceMatrix = S['PublicPriceMatrix'];
export type PublicOccupancy = S['PublicOccupancy'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;

/** Refetch interval of the aggregate occupancy on the public page (the server caches it 60 s). */
export const PUBLIC_OCCUPANCY_REFETCH_MS = 60_000;

/** The profile draft (marina.read). */
export function useMarinaProfile(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.profile(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/profile`, { params: { path: { marinaId } } })),
  });
}

function useProfileMutation<V>(
  marinaId: string,
  fn: (api: ReturnType<typeof useApi>, v: V) => Promise<MarinaProfile>,
) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: V) => fn(api, v),
    onSuccess: (data) => {
      qc.setQueryData(queryKeys.profile(marinaId), data);
      return qc.invalidateQueries({ queryKey: queryKeys.profile(marinaId) });
    },
  });
}

/** Saves the draft (marina.configure; version 0 for the first save). */
export function useSaveMarinaProfile(marinaId: string) {
  return useProfileMutation(marinaId, (api, v: { version: number; body: MarinaProfileInput }) =>
    unwrap(
      api.PUT(`${M}/profile`, {
        params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
        body: v.body,
      }),
    ),
  );
}

/** Publishes or withdraws the profile. */
export function usePublishMarinaProfile(marinaId: string) {
  return useProfileMutation(marinaId, (api, v: { version: number; publish: boolean }) => {
    const params = { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } };
    return v.publish
      ? unwrap(api.POST(`${M}/profile/publish`, { params }))
      : unwrap(api.POST(`${M}/profile/unpublish`, { params }));
  });
}

/** Uploads a photo (raw JPEG/PNG body, re-encoded by the server); reference it with a save. */
export function useUploadProfilePhoto(marinaId: string) {
  return useMutation({
    mutationFn: async (file: Blob) =>
      (
        await uploadBinary<ProfilePhotoFile>(
          `/api/admin/v1/marinas/${marinaId}/profile/photos`,
          file,
          {
            method: 'POST',
            contentType: file.type || 'image/jpeg',
            headers: { 'Idempotency-Key': newIdempotencyKey() },
          },
        )
      ).data,
  });
}

/** Deletes an uploaded photo and removes it from the saved profile. */
export function useDeleteProfilePhoto(marinaId: string) {
  return useProfileMutation(marinaId, (api, v: { version: number; fileId: string }) =>
    unwrap(
      api.DELETE(`${M}/profile/photos/{fileId}`, {
        params: {
          path: { marinaId, fileId: v.fileId },
          header: { 'If-Match': ifMatch(v.version) },
        },
      }),
    ),
  );
}

/** URL of an uploaded photo for the panel (marina.read). */
export function profilePhotoUrl(marinaId: string, fileId: string): string {
  return `/api/admin/v1/marinas/${marinaId}/profile/photos/${fileId}`;
}

/** The public JSON and JSON-LD the draft would publish (marina.configure; never cached). */
export function useMarinaProfilePreview(marinaId: string | null, lang?: 'it' | 'en') {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.profilePreview(marinaId ?? '', lang),
    enabled: Boolean(marinaId),
    retry: false,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/profile/preview`, {
          params: { path: { marinaId: marinaId ?? '' }, query: { lang } },
        }),
      ),
  });
}

/** The published profile of a slug in a language (public, cacheable). */
export function usePublicProfile(slug: string, lang?: 'it' | 'en', enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.publicProfile(slug, lang),
    enabled,
    retry: false,
    queryFn: () =>
      unwrap(
        api.GET('/api/public/v1/{slug}/profile', { params: { path: { slug }, query: { lang } } }),
      ),
  });
}

/** The simplified map (only when the section is on). */
export function usePublicProfileMap(slug: string, enabled: boolean) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.publicProfileMap(slug),
    enabled,
    retry: false,
    staleTime: 3_600_000,
    queryFn: () =>
      unwrap(api.GET('/api/public/v1/{slug}/profile/map', { params: { path: { slug } } })),
  });
}

/** The transit price matrix (only when the section is on). */
export function usePublicProfilePrices(
  slug: string,
  lang: 'it' | 'en' | undefined,
  enabled: boolean,
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.publicProfilePrices(slug, lang),
    enabled,
    retry: false,
    staleTime: 3_600_000,
    queryFn: () =>
      unwrap(
        api.GET('/api/public/v1/{slug}/profile/prices', {
          params: { path: { slug }, query: { lang } },
        }),
      ),
  });
}

/** The aggregate occupancy, refetched every 60 s while the page is visible. */
export function usePublicOccupancy(slug: string, enabled: boolean) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.publicOccupancy(slug),
    enabled,
    retry: false,
    refetchInterval: PUBLIC_OCCUPANCY_REFETCH_MS,
    refetchIntervalInBackground: false,
    queryFn: () =>
      unwrap(api.GET('/api/public/v1/{slug}/occupancy', { params: { path: { slug } } })),
  });
}
