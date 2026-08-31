import { searchAssets } from '@immich/sdk';
import { authenticate } from '$lib/utils/auth';
import { getFormatter } from '$lib/utils/i18n';
import type { PageLoad } from './$types';

export const load = (async ({ url }) => {
  await authenticate(url);

  // Most recent first so the picker feels like the timeline head.
  const { assets } = await searchAssets({ metadataSearchDto: { size: 1000, order: 'desc' } });
  const $t = await getFormatter();

  return {
    assets,
    meta: {
      title: `${$t('create_album')}`,
    },
  };
}) satisfies PageLoad;
